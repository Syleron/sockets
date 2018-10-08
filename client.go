package sockets

import "github.com/gorilla/websocket"

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

