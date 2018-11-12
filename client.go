/**
MIT License

Copyright (c) 2018 Andrew Zak <andrew@linux.com>

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
	"github.com/gorilla/websocket"
	"time"
)

type Client struct {
	Username string `json:"username"`
	connected bool
	Connections []*websocket.Conn
}

func(c *Client) Emit(data string) {
	// We are sending this to a single user
	// but on multiple connections.
	for _, conn := range c.Connections {
		conn.WriteJSON(data)
	}
}

func (c *Client) RemoveConnection(conn *websocket.Conn) {
	for i, connection := range c.Connections {
		if connection == conn {
			c.Connections[i] = c.Connections[len(c.Connections)-1]
			c.Connections[len(c.Connections)-1] = &websocket.Conn{}
			c.Connections = c.Connections[:len(c.Connections)-1]
		}
	}
}

func (c *Client) PingHandler() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Emit("ping")
		}
	}
}

