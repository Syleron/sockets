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
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

type DataHandler interface {
	// Client connected handler
	NewConnection(ctx *Context)
	// Client disconnect handler
	ConnectionClosed(ctx *Context)
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

type Sockets struct {
	clients       map[string]*Client
	broadcastChan chan Broadcast
	interrupt     chan os.Signal
	handler       DataHandler
	jwtKey        string
	sync.Mutex
}

type Context struct {
	*Connection
	*Client
	UUID string
}

type Broadcast struct {
	message *common.Message
	context *Context
}

type Room struct {
	Name    string
	Channel string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func New(jwtKey string, handler DataHandler) *Sockets {
	// Setup the sockets object
	sockets := &Sockets{}
	sockets.clients = make(map[string]*Client)
	sockets.broadcastChan = make(chan Broadcast)
	sockets.interrupt = make(chan os.Signal, 1)
	signal.Notify(sockets.interrupt, os.Interrupt)
	sockets.handler = handler
	// Set the JWT token key
	sockets.jwtKey = jwtKey
	// OS interrupt handling
	go sockets.InterruptHandler()
	// return our object
	return sockets
}

func (s *Sockets) Close() {
	s.Close()
}

func (s *Sockets) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}

func (s *Sockets) InterruptHandler() {
	for {
		select {
		// OS interrupt signal
		case <-s.interrupt:
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			for _, client := range s.clients {
				for _, conn := range client.connections {
					if conn.Conn == nil {
						continue
					}
					conn.Conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := conn.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
						return
					}
				}
			}
			os.Exit(0)
		}
	}
}

func (s *Sockets) HandleConnection(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	query := r.URL.Query()
	jwtString := query.Get("jwt")

	// Check to see if the JWT is undefined
	if jwtString == "undefined" || jwtString == "" {
		// TODO: Needs proper error reporting
		ws.Close()
		return
	}

	success, jwt := common.DecodeJWT(jwtString, s.jwtKey)

	// Unable to decode JWT
	if !success {
		// TODO: Needs proper error reporting
		ws.Close()
		return
	}

	// Define our connection client
	var client *Client
	var uuid string

	// TODO: Should split this out
	if s.CheckIfClientExists(jwt.Username) {
		client = s.clients[jwt.Username]
		uuid = client.addConnection(&Connection{
			Conn: ws,
			Room: &Room{},
		})
	} else {
		client = &Client{}
		uuid = client.addConnection(&Connection{
			Conn: ws,
			Room: &Room{},
		})
		client.Username = jwt.Username
		client.connected = true
		s.addClient(client)
	}

	// Build our connection context
	context := &Context{
		Connection: &Connection{
			Conn: ws,
		},
		UUID: uuid,
		Client: client,
	}

	// Pass onto the interface function
	s.handler.NewConnection(context)

	// Handle PONG and connection timeouts
	ws.SetReadLimit(maxMessageSize)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Handle our incoming message
	for {
		var msg common.Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			s.closeWS(s.clients[jwt.Username], ws)
			break
		}
		EventHandler(&msg, context)
	}
}

func (s *Sockets) BroadcastToRoom(roomName, event string, data, options interface{}) {
	for _, client := range s.clients {
		for _, conn := range client.connections {
			if conn.Room.Name == roomName {
				var message common.Response
				message.EventName = event
				message.Data = data
				if conn.Conn == nil {
					continue
				}
				if err := conn.Conn.WriteJSON(message); err != nil {
					// TODO: Needs proper error reporting
					continue
				}
			}
		}
	}
}

func (s *Sockets) BroadcastToRoomChannel(roomName, channelName, event string, data, options interface{}) {
	for _, client := range s.clients {
		for _, conn := range client.connections {
			if conn.Room.Name == roomName && conn.Room.Channel == channelName {
				var message common.Response
				message.EventName = event
				message.Data = data
				if conn.Conn == nil {
					continue
				}
				if err := conn.Conn.WriteJSON(message); err != nil {
					// TODO: Needs proper error reporting
					continue
				}
			}
		}
	}
}

func (s *Sockets) CheckIfClientExists(username string) bool {
	if s.clients[username] != nil {
		return true
	}
	return false
}

func (s *Sockets) GetUserRoom(username, uuid string) (string) {
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
			return conn.Room.Name
		}
	}
	return ""
}

func (s *Sockets) LeaveRoom(username, uuid string) {
	s.Lock()
	defer s.Unlock()
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
			conn.Room = nil
		}
	}
}

func (s *Sockets) GetUserRoomChannel(username, uuid string) (string) {
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
				return conn.Room.Channel
		}
	}
	return ""
}

func (s *Sockets) JoinRoom(username, room, uuid string) {
	s.Lock()
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
			conn.Room = &Room{
				Name: room,
			}
		}
	}
	s.Unlock()
}

func (s *Sockets) JoinRoomChannel(username, channel, uuid string) {
	s.Lock()
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
			conn.Room.Channel = channel
		}
	}
	s.Unlock()
}

func (s *Sockets) UserInARoom(username, uuid string) (bool) {
	if client := s.clients[username]; client != nil {
		if conn := client.connections[uuid]; conn != nil {
			if conn.Room == nil {
				return false
			}
		}
	}
	return true
}

func (s *Sockets) closeWS(client *Client, connection *websocket.Conn) {
	// Do we have a client and a connection?
	if client != nil && connection != nil {
		uuid, conn := client.getConnection(connection)
		s.handler.ConnectionClosed(&Context{
			Connection: conn,
			Client:     client,
		})
		// Remove our connection from the user connection list.
		client.removeConnection(uuid)
		// Determine whether we need to remove the user from the online list
		if len(client.connections) <= 0 {
			// Remove from local online list
			s.removeClient(client)
		}
		// Close the Connection
		connection.Close()
	} else {
		s.handler.ConnectionClosed(&Context{
			Connection: nil,
			Client:     client,
		})
		if connection != nil {
			connection.Close()
		}
	}
}

func (s *Sockets) addClient(client *Client) {
	s.Lock()
	s.clients[client.Username] = client
	s.Unlock()
}

func (s *Sockets) removeClient(client *Client) {
	s.Lock()
	delete(s.clients, client.Username)
	s.Unlock()
}
