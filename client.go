/**
MIT License

Copyright (c) 2018-2019 Andrew Zak <andrew@linux.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package sockets

import (
	"github.com/Syleron/sockets/common"
	"github.com/gorilla/websocket"
	"github.com/rs/xid"
	"sync"
	"time"
)

type Client struct {
	Username    string `json:"username"`
	connected   bool
	connections map[string]*Connection // Indexed by UUID
	sync.Mutex
}

type Connection struct {
	//UUID string `json:"uuid"`
	Conn *websocket.Conn
	Room *Room
}

func (c *Connection) pongHandler() {
	ticker := time.NewTicker(pingPeriod)

	defer func() {
		ticker.Stop()
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

func (c *Client) addConnection(newConnection *Connection) string {
	c.Lock()
	defer c.Unlock()
	if c.connections == nil {
		c.connections = make(map[string]*Connection)
	}
	// Generate an unique ID
	uuid := xid.New().String()
	// Start our pong handler
	go newConnection.pongHandler()
	// Append our connection
	c.connections[uuid] = newConnection
	return uuid
}

func (c *Client) removeConnection(uuid string) {
	c.Lock()
	delete(c.connections, uuid)
	c.Unlock()
}

func (c *Client) Emit(msg *common.Message) {
	// We are sending this to a single user
	// but on multiple connections.
	for _, connection := range c.connections {
		connection.Conn.WriteJSON(msg)
	}
}

func (c *Client) getConnection(conn *websocket.Conn) (string, *Connection) {
	for uuid, c := range c.connections {
		if c.Conn == conn {
			return uuid, c
		}
	}
	return "", nil
}

