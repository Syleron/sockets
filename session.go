package sockets

import (
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/common"
	"sync"
)

type Session struct {
	Username string
	connections map[string]*Connection
	sync.Mutex
}

func (s *Session) addConnection(newConnection *Connection) {
	s.Lock()
	defer s.Unlock()
	if s.connections == nil {
		s.connections = make(map[string]*Connection)
	}
	// Start our pong handler
	//go newConnection.pongHandler()
	// Append our connection
	s.connections[newConnection.UUID] = newConnection
}

func (s *Session) removeConnection(uuid string) {
	s.Lock()
	// Remove our connection from our connections array
	delete(s.connections, uuid)
	s.Unlock()
}

func (s *Session) Emit(msg *common.Message) {
	// We are sending this to a single user
	// but on multiple connections.
	for _, connection := range s.connections {
		if err := connection.Emit(msg); err != nil {
			continue
		}
	}
}

func (s *Session) getConnection(conn *websocket.Conn) (string, *Connection) {
	for uuid, c := range s.connections {
		if c.Conn == conn {
			return uuid, c
		}
	}
	return "", nil
}