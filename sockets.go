// MIT License
//
// Copyright (c) 2018-2024 Andrew Zak <andrew@linux.com>
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
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/v2/common"
	"log/slog"
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
	handler       DataHandler
	config        *Config
	sync.RWMutex
}

type Context struct {
	*Connection
	UUID      string
	PeerCerts []*x509.Certificate
}

type Broadcast struct {
	message *common.Message //nolint:unused // unexported, kept to leave the exported Broadcast type unchanged
	context *Context        //nolint:unused // unexported, kept to leave the exported Broadcast type unchanged
}

type Room struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Add a check for the origin of the request to prevent CSRF attacks.
		return true
	},
}

func New(handler DataHandler, c *Config) *Sockets {
	c.MergeDefaults()

	sockets := &Sockets{
		Connections:   make(map[string]*Connection),
		Sessions:      make(map[string]*Session),
		broadcastChan: make(chan Broadcast),
		interrupt:     make(chan os.Signal, 1),
		handler:       handler,
		config:        c,
	}

	signal.Notify(sockets.interrupt, os.Interrupt)
	go sockets.manageInterrupts()

	return sockets
}

func (s *Sockets) Close() {
	s.RLock()
	defer s.RUnlock()

	for _, c := range s.Connections {
		if c.Conn != nil {
			_ = c.Conn.Close() // best effort: the process exits below
		}
	}

	s.logger().Info("all connections closed")
	os.Exit(0)
}

// logger returns the logger from Config, or slog.Default() when unset.
func (s *Sockets) logger() *slog.Logger {
	return s.config.logger()
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

func (s *Sockets) manageInterrupts() {
	<-s.interrupt
	s.logger().Info("received interrupt signal, shutting down")
	s.Close()
}

func (s *Sockets) HandleConnection(w http.ResponseWriter, r *http.Request, realIP string) error {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger().Warn("websocket upgrade failed", slog.Any("error", err))
		return fmt.Errorf("websocket upgrade error: %w", err)
	}
	newConnection := NewConnection()
	newConnection.Conn = ws
	newConnection.RealIP = determineRealIP(s.logger(), ws, realIP)
	newConnection.Status = true

	peerCerts := getPeerCertificates(r)

	s.addConnection(newConnection.UUID, newConnection)

	context := &Context{
		Connection: newConnection,
		UUID:       newConnection.UUID,
		PeerCerts:  peerCerts,
	}

	s.handler.NewConnection(context)

	ws.SetReadLimit(s.config.ReadLimitSize)
	if err := ws.SetReadDeadline(time.Now().Add(s.config.PongWait)); err != nil {
		s.logger().Warn("failed to set read deadline", slog.String("uuid", newConnection.UUID), slog.Any("error", err))
	}
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(s.config.PongWait))
	})

	return s.handleMessages(ws, context)
}

func (s *Sockets) handleMessages(ws *websocket.Conn, context *Context) error {
	for {
		var msg common.Message
		if err := ws.ReadJSON(&msg); err != nil {
			s.closeWS(context.Connection)
			return fmt.Errorf("error reading JSON: %w", err)
		}
		dispatchEvent(s.logger(), &msg, context)
	}
}

func (s *Sockets) Broadcast(event string, data interface{}) {
	s.broadcastHelper(func(c *Connection) bool {
		return true // Always true, since we're broadcasting to all
	}, event, data)
}

func (s *Sockets) BroadcastToRoom(roomName, event string, data interface{}, ctx *Context) {
	s.broadcastHelper(func(c *Connection) bool {
		return c.Room != nil && c.Room.Name == roomName && c.UUID != ctx.UUID
	}, event, data)
}

func (s *Sockets) BroadcastToRoomChannel(roomName, channelName, event string, data interface{}, ctx *Context) {
	s.broadcastHelper(func(c *Connection) bool {
		return c.Room != nil && c.Room.Name == roomName && c.Room.Channel == channelName && c.UUID != ctx.UUID
	}, event, data)
}

func (s *Sockets) broadcastHelper(filter func(*Connection) bool, event string, data interface{}) {
	s.RLock()
	defer s.RUnlock()

	for _, c := range s.Connections {
		if c.Conn == nil {
			continue
		}

		if filter(c) {
			message := common.Response{
				EventName: event,
				Data:      data,
			}

			if err := c.Emit(message); err != nil {
				s.logger().Warn("failed to emit message", slog.String("uuid", c.UUID), slog.Any("error", err))
				continue
			}
		}
	}
}

func (s *Sockets) GetUserRoom(username, uuid string) (string, error) {
	if username == "" || uuid == "" {
		return "", errors.New("invalid input: username or UUID is empty")
	}

	s.RLock()
	defer s.RUnlock()

	if session, ok := s.Sessions[username]; ok {
		if conn, ok := session.connections[uuid]; ok && conn.Room != nil {
			return conn.Room.Name, nil
		}
	}

	return "", fmt.Errorf("unable to find room for user %s with UUID %s", username, uuid)
}

func (s *Sockets) JoinRoom(room, uuid string) error {
	if room == "" || uuid == "" {
		return errors.New("invalid input: room or UUID is empty")
	}

	s.Lock()
	defer s.Unlock()

	if conn, ok := s.Connections[uuid]; ok {
		conn.Room = &Room{Name: room}
		s.logger().Debug("joined room", slog.String("uuid", uuid), slog.String("room", room))
		return nil
	}

	return fmt.Errorf("unable to find client connection for UUID %s", uuid)
}

func (s *Sockets) LeaveRoom(uuid string) {
	if uuid == "" {
		s.logger().Warn("attempted to leave room with empty UUID")
		return
	}

	s.Lock()
	defer s.Unlock()

	if conn, ok := s.Connections[uuid]; ok && conn.Room != nil {
		conn.Room = &Room{} // Effectively "leaving" the room by resetting it
		s.logger().Debug("left room", slog.String("uuid", uuid))
	}
}

func (s *Sockets) JoinRoomChannel(channel, uuid string) error {
	if channel == "" || uuid == "" {
		return errors.New("invalid input: channel or UUID is empty")
	}

	s.Lock()
	defer s.Unlock()

	if conn, ok := s.Connections[uuid]; ok && conn.Room != nil {
		conn.Room.Channel = channel
		s.logger().Debug("joined channel", slog.String("uuid", uuid), slog.String("channel", channel))
		return nil
	}
	return fmt.Errorf("unable to find client connection for UUID %s", uuid)
}

func (s *Sockets) manageSessionAndConnection(conn *Connection) {
	s.Lock()
	defer s.Unlock()

	username := conn.Username
	uuid := conn.UUID

	// Check if the session exists and manage the session if it does
	if session, exists := s.Sessions[username]; exists {
		delete(session.connections, uuid) // Remove connection from session

		// If no more connections are left in the session, delete the session
		if len(session.connections) == 0 {
			if err := s.deleteSession(username); err != nil {
				s.logger().Warn("failed to delete session", slog.String("username", username), slog.Any("error", err))
			}
		}
	}

	// Remove the connection from the global list
	delete(s.Connections, uuid)
}

func (s *Sockets) deleteSession(username string) error {
	if _, exists := s.Sessions[username]; !exists {
		return fmt.Errorf("no session exists for username: %s", username)
	}

	delete(s.Sessions, username)
	return nil
}

func (s *Sockets) addConnection(uuid string, conn *Connection) {
	if uuid == "" || conn == nil {
		s.logger().Warn("addConnection: empty UUID or nil connection")
		return
	}

	s.Lock()
	defer s.Unlock()

	// Check if there's already an existing connection with the same UUID
	if existing, exists := s.Connections[uuid]; exists {
		s.logger().Warn("connection with UUID already exists, closing existing connection", slog.String("uuid", uuid))
		if err := existing.Conn.Close(); err != nil { // Ensure the existing connection is properly closed
			s.logger().Debug("failed to close existing connection", slog.String("uuid", uuid), slog.Any("error", err))
		}
	}

	// Start our pong handler
	go conn.pongHandler(s.config.PingPeriod)

	// Append our connection
	s.Connections[uuid] = conn
	s.logger().Debug("connection added", slog.String("uuid", uuid))
}

func (s *Sockets) AddSession(username string, conn *Connection) error {
	if username == "" || conn == nil {
		return errors.New("invalid username or connection")
	}

	s.Lock()
	defer s.Unlock()

	// Do we already have a session?
	if _, exists := s.Sessions[username]; exists {
		return errors.New("session already exists for this user")
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

func (s *Sockets) CheckIfSessionExists(username string) bool {
	return s.Sessions[username] != nil
}

func (s *Sockets) UpdateSession(username string, conn *Connection) error {
	if username == "" || conn == nil {
		return errors.New("invalid username or connection")
	}

	s.Lock()
	defer s.Unlock()

	session, exists := s.Sessions[username]
	if !exists {
		s.logger().Debug("update session failed: no session exists", slog.String("username", username))
		return errors.New("no session exists for this user")
	}

	// Add our connection to our session
	session.addConnection(conn)
	// Add our session to our connection
	conn.addSession(session)

	s.logger().Debug("session updated", slog.String("username", username))
	// success
	return nil
}

func (s *Sockets) DeleteSession(username string) error {
	if username == "" {
		return errors.New("invalid username")
	}

	s.Lock()
	defer s.Unlock()

	if _, exists := s.Sessions[username]; !exists {
		s.logger().Debug("delete session failed: no session exists", slog.String("username", username))
		return errors.New("no session exists for this user")
	}

	delete(s.Sessions, username)
	s.logger().Debug("session deleted", slog.String("username", username))

	return nil
}

func (s *Sockets) closeWS(conn *Connection) {
	// Check if the connection is non-nil
	if conn == nil {
		s.logger().Warn("attempted to close a nil connection")
		return
	}

	// Notify handler that the connection is closing
	s.handler.ConnectionClosed(&Context{
		Connection: conn,
		UUID:       conn.UUID,
	})

	// Attempt to close the WebSocket connection
	if err := conn.Conn.Close(); err != nil {
		s.logger().Warn("failed to close websocket connection", slog.String("uuid", conn.UUID), slog.Any("error", err))
	}

	// Safely manage the session and connection removal
	s.manageSessionAndConnection(conn)
}
