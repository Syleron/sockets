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
	}
}

func (c *Connection) AddSession(session *Session) {
	c.Session = session
}

func (c *Connection) ClearSession(session *Session) {
	c.Session = nil
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

func (c *Connection) Emit(msg interface{}) error {
	// We are sending this to a single user
	// but on multiple connections.
	if err := c.Conn.WriteJSON(msg); err != nil {
		return err
	}
	return nil
}
