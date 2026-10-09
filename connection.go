// MIT License
//
// Copyright (c) 2022 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
/// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package sockets

import (
	"github.com/gorilla/websocket"
	"github.com/rs/xid"
	"sync"
	"time"
)

type Connection struct {
	UUID   string
	Conn   *websocket.Conn
	Status bool  `json:"status"`
	Room   *Room `json:"room"`
	Data   map[string]interface{}
	//The connection Source address determined by the user.
	RealIP string
	sync.RWMutex
	*Session

	// writeMu serialises data writes to Conn. gorilla/websocket allows one
	// concurrent writer per connection; Emit is called from event handlers,
	// broadcasts and Session.Emit on different goroutines. It is separate from
	// the embedded RWMutex (which guards Data) so SetData/GetData never wait
	// behind a slow write. Lock ordering: never acquire the Sockets registry
	// lock or a Session lock while holding writeMu.
	writeMu sync.Mutex
	// writeWait bounds each write (Config.WriteWait). Zero or negative means
	// no deadline. Set before the connection is shared and read-only
	// afterwards.
	writeWait time.Duration
}

func NewConnection() *Connection {
	// Generate an unique ID
	uuid := xid.New().String()

	return &Connection{
		UUID: uuid,
		Room: &Room{
			Name:    "",
			Channel: "",
		},
		Session: &Session{
			Username:    "",
			connections: nil,
			Mutex:       sync.Mutex{},
		},
		Data: map[string]interface{}{},
	}
}

// Emit writes msg to the connection as a JSON text message. It is safe to call
// from multiple goroutines: writes to the same connection are serialised.
// When Config.WriteWait is positive (the default is 10s), a write that does
// not complete in time fails, and gorilla/websocket fails every later write
// on the connection, so the keepalive closes it.
//
// Write to the connection only through Emit: writing to Conn directly
// bypasses the serialisation.
func (c *Connection) Emit(msg interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// SetWriteDeadline is a write method in gorilla/websocket, so it must be
	// called under writeMu too. It only records the deadline for the next
	// frames (no syscall), so it is reset afterwards to leave no stale
	// deadline behind for anything else that writes to Conn.
	if c.writeWait > 0 {
		if err := c.Conn.SetWriteDeadline(time.Now().Add(c.writeWait)); err != nil {
			return err
		}
		defer func() { _ = c.Conn.SetWriteDeadline(time.Time{}) }()
	}
	return c.Conn.WriteJSON(msg)
}

func (c *Connection) SetData(key string, value interface{}) {
	c.Lock()
	defer c.Unlock()
	c.Data[key] = value
}

func (c *Connection) GetData(key string) interface{} {
	c.RLock()
	defer c.RUnlock()
	return c.Data[key]
}

func (c *Connection) ClearSession() {
	c.Session = nil
}

func (c *Connection) addSession(session *Session) {
	c.Session = session
}

// pongHandler sends a ping every pingPeriod until a ping fails, then closes
// the connection. Pings use WriteControl, which gorilla/websocket documents as
// safe to call concurrently with the other write methods, so they do not take
// writeMu and are not delayed behind a large or slow data write beyond
// writeWait.
func (c *Connection) pongHandler(pingPeriod, writeWait time.Duration) {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		// Set our connection state
		c.Status = false
		// Stop our ticker
		ticker.Stop()
		// Close our connection; the read loop observes the error and cleans up.
		// TODO: This may cause issues as it may not clear up our connections array
		_ = c.Conn.Close()
	}()

	// Send a ping message depicted by our ticker
	for range ticker.C {
		// Periodically send a ping message
		var deadline time.Time // zero: no deadline
		if writeWait > 0 {
			deadline = time.Now().Add(writeWait)
		}
		if err := c.Conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
			return
		}
	}
}
