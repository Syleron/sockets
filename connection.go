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
	*Session
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

func (c *Connection) Emit(msg interface{}) error {
	// We are sending this to a single user
	// but on multiple connections.
	if err := c.Conn.WriteJSON(msg); err != nil {
		return err
	}
	return nil
}

func (c *Connection) SetData(key string, value interface{}) {
	c.Data[key] = value
}

func (c *Connection) GetData(key string) interface{} {
	return c.Data[key]
}

func (c *Connection) ClearSession() {
	c.Session = nil
}

func (c *Connection) addSession(session *Session) {
	c.Session = session
}

func (c *Connection) pongHandler() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		// Set our connection state
		c.Status = false
		// Stop our ticker
		ticker.Stop()
		// Close our connection
		// TODO: This may cause issues as it may not clear up our connections array
		c.Conn.Close()
	}()

	for {
		select {
		// Send a ping message depicted by our ticker
		case <-ticker.C:
			// Periodically send a ping message
			if err := c.Conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
