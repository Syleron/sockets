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
	"sync"
	"time"
)

type Client struct {
	Username    string `json:"username"`
	connected   bool
	connections []*Connection
	sync.Mutex
}

type Connection struct {
	UUID string `json:"uuid"`
	Conn *websocket.Conn
}

func (c *Client) addConnection(newConnection *Connection) {
	c.Lock()
	c.connections = append(
		c.connections,
		newConnection,
	)
	c.Unlock()
}

func (c *Client) removeConnection(conn *websocket.Conn) {
	c.Lock()
	for i, connection := range c.connections {
		if connection.Conn == conn {
			c.connections = append(c.connections[:i], c.connections[i+1:]...)
		}
	}
	c.Unlock()
}

func (c *Client) Emit(msg *common.Message) {
	// We are sending this to a single user
	// but on multiple connections.
	for _, connection := range c.connections {
		connection.Conn.WriteJSON(msg)
	}
}

func (c *Client) getConnection(conn *websocket.Conn) *Connection {
	for _, c := range c.connections {
		if c.Conn == conn {
			return c
		}
	}
	return nil
}

/**
TODO: Replace with gorillas internal ping/poing handler(s)
*/
func (c *Client) pingHandler() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Emit(&common.Message{
				EventName: "ping",
			})
		}
	}
}
