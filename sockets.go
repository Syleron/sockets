// MIT License
//
// Copyright (c) 2018-2022 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
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
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/common"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

type DataHandler interface {
	// NewConnection Client connected handler
	NewConnection(ctx *Context)
	// ConnectionClosed Client disconnect handler
	ConnectionClosed(ctx *Context)
}

type Sockets struct {
	Connections   map[string]*Connection
	Sessions      map[string]*Session
	broadcastChan chan Broadcast
	interrupt     chan os.Signal
	handler DataHandler
	config  *Config
	sync.Mutex
}

type Context struct {
	*Connection
	UUID string
}

type Broadcast struct {
	message *common.Message
	context *Context
}

type Room struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

type Config struct {
	// Time allowed to write a message to the peer.
	WriteWait time.Duration
	// Time allowed to read the next pong message from the peer.
	PongWait time.Duration
	// Send pings to peer with this period. Must be less than pongWait.
	PingPeriod time.Duration
	// Maximum message size allowed from peer.
	ReadLimitSize int64
}

func newConfig(c *Config) *Config {
	if c.WriteWait == 0 {
		c.WriteWait = time.Second * 10
	}
	if c.PongWait == 0 {
		c.PongWait = time.Second * 60
	}
	if c.PingPeriod == 0 {
		c.PingPeriod = (c.PongWait * 9) / 10
	}
	if c.ReadLimitSize == 0 {
		c.ReadLimitSize = 2560
	}
	return c
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func New(handler DataHandler, c *Config) *Sockets {
	// Setup the sockets object
	sockets := &Sockets{}
	sockets.config = newConfig(c)
	sockets.Connections = make(map[string]*Connection)
	sockets.Sessions = make(map[string]*Session)
	sockets.broadcastChan = make(chan Broadcast)
	sockets.interrupt = make(chan os.Signal, 1)
	signal.Notify(sockets.interrupt, os.Interrupt)
	sockets.handler = handler
	// OS interrupt handling
	go sockets.InterruptHandler()

	// return our object
	return sockets
}

func (s *Sockets) Close() {
	s.Close()
}

func (s *Sockets) HandleEvent(pattern string, handler EventFunc, protected bool) {
	if events == nil {
		events = make(map[string]*Event)
	}
	events[pattern] = &Event{
		EventFunc: handler,
		Protected: protected,
	}
}

func (s *Sockets) InterruptHandler() {
	for {
		select {
		// OS interrupt signal
		case <-s.interrupt:
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			for _, c := range s.Connections {
				if c.Conn == nil {
					continue
				}
				c.Conn.SetWriteDeadline(time.Now().Add(s.config.WriteWait))
				if err := c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					return
				}
			}
			os.Exit(0)
		}
	}
}

func (s *Sockets) HandleConnection(w http.ResponseWriter, r *http.Request) error {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	newConnection := NewConnection(&)

	// Set our Connection
	newConnection.Conn = ws

	// Set our connection status
	newConnection.Status = true

	// Add our connection to our array
	s.addConnection(newConnection.UUID, newConnection)

	// Build our connection context
	context := &Context{
		Connection: newConnection,
		UUID:       newConnection.UUID,
	}

	// Pass onto the interface function
	s.handler.NewConnection(context)

	// Handle PONG and connection timeouts
	ws.SetReadLimit(s.config.ReadLimitSize)
	ws.SetReadDeadline(time.Now().Add(s.config.PongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(s.config.PongWait))
		return nil
	})

	// Handle our incoming message
	for {
		var msg common.Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			// TODO
			fmt.Println(err)
			s.closeWS(newConnection)
			break
		}
		EventHandler(&msg, context)
	}
	return nil
}

func (s *Sockets) Broadcast(event string, data, options interface{}) {
	for _, c := range s.Connections {
		if c.Conn == nil {
			continue
		}
		var message common.Response
		message.EventName = event
		message.Data = data
		if c.Conn == nil {
			continue
		}
		if err := c.Emit(message); err != nil {
			// TODO: Needs proper error reporting
			continue
		}
	}
}

func (s *Sockets) BroadcastToRoom(roomName, event string, data interface{}, ctx *Context) {
	for _, c := range s.Connections {
		if c.Conn == nil || c.Room == nil {
			continue
		}
		if c.Room.Name == roomName && c.UUID != ctx.UUID {
			var message common.Response
			message.EventName = event
			message.Data = data
			if err := c.Emit(message); err != nil {
				continue
			}
		}
	}
}

func (s *Sockets) BroadcastToRoomChannel(roomName, channelName, event string, data interface{}, ctx *Context) {
	for _, c := range s.Connections {
		if c.Conn == nil || c.Room == nil {
			continue
		}
		if c.Room.Name == roomName &&
			c.Room.Channel == channelName &&
			c.UUID != ctx.UUID {
			var message common.Response
			message.EventName = event
			message.Data = data
			if err := c.Emit(message); err != nil {
				continue
			}
		}
	}
}

func (s *Sockets) CheckIfSessionExists(username string) bool {
	if s.Sessions[username] != nil {
		return true
	}
	return false
}

func (s *Sockets) GetUserRoom(username, uuid string) (string, error) {
	if session := s.Sessions[username]; session != nil {
		if conn := session.connections[uuid]; conn != nil {
			return conn.Room.Name, nil
		}
	}
	return "", errors.New("unable to find room for user " + username + " with UUID " + uuid)
}

func (s *Sockets) LeaveRoom(uuid string) {
	s.Lock()
	defer s.Unlock()
	if conn := s.Connections[uuid]; conn != nil {
		conn.Room = &Room{}
	}
}

func (s *Sockets) JoinRoom(room, uuid string) error {
	s.Lock()
	defer s.Unlock()
	if conn := s.Connections[uuid]; conn != nil {
		conn.Room = &Room{
			Name: room,
		}
	} else {
		return errors.New("unable to find client connection")
	}
	return nil
}

func (s *Sockets) JoinRoomChannel(channel, uuid string) error {
	s.Lock()
	defer s.Unlock()
	if conn := s.Connections[uuid]; conn != nil {
		conn.Room.Channel = channel
	} else {
		return errors.New("unable to find client connection")
	}
	return nil
}

func (s *Sockets) closeWS(conn *Connection) {
	// Do we have a client and a connection?
	if conn != nil {
		s.handler.ConnectionClosed(&Context{
			Connection: conn,
			UUID:       conn.UUID,
		})
		// Close the Connection
		if err := conn.Conn.Close(); err != nil {
			// TODO: Log?
		}
		// Remove our connection from our session
		if s.CheckIfSessionExists(conn.Username) {
			// Check how many connections we have
			if len(s.Sessions[conn.Username].connections) == 1 {
				// Delete our session
				if err := s.DeleteSession(conn.Username); err != nil {
					// TODO: Log?
				}
			}
		}
		// Remove our connection from the user connection list.
		s.removeConnection(conn.UUID)
	}
}

func (s *Sockets) addConnection(uuid string, conn *Connection) {
	s.Lock()
	defer s.Unlock()
	// Start our pong handler
	go conn.pongHandler()
	// Append our connection
	s.Connections[uuid] = conn
}

func (s *Sockets) removeConnection(uuid string) {
	s.Lock()
	defer s.Unlock()
	delete(s.Connections, uuid)
}

func (s *Sockets) AddSession(username string, conn *Connection) error {
	s.Lock()
	defer s.Unlock()
	// Do we already have a session?
	if s.CheckIfSessionExists(username) {
		return errors.New("session object already exists")
	}
	// Define our new session
	newSession := &Session{
		Username:    username,
		connections: make(map[string]*Connection),
	}
	// Add our connection to our session
	newSession.connections[conn.UUID] = conn
	// Add our session reference to our connection
	conn.addSession(newSession)
	// Add our session to our sockets store
	s.Sessions[username] = newSession
	// success
	return nil
}

func (s *Sockets) UpdateSession(username string, conn *Connection) error {
	s.Lock()
	defer s.Unlock()
	if !s.CheckIfSessionExists(username) {
		return errors.New("session does not exist for this user")
	}
	// Get our session object
	session := s.Sessions[username]
	// Add our connection to our session
	session.addConnection(conn)
	// success
	return nil
}

func (s *Sockets) DeleteSession(username string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.Sessions, username)
	return nil
}
