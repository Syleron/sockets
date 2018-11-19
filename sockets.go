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
	"net/http"
	"github.com/rs/xid"
		"sync"
		"encoding/json"
)

type sockets interface {
	// Close websocket connection
	Close()
	// Function used to define a WS event listener
	HandleEvent(pattern string, handler EventFunc)
	// Handle incoming WS connections via a router
	HandleConnections(w http.ResponseWriter, r *http.Request)
	// Function used to broadcast an event to members defined in a room
	BroadcastToRoom(roomName, event string, data, options interface{})
	// Function used to broadcast an event to members defined in a room's channel
	BroadcastToRoomChannel(roomName, channelName, event string, data, options interface{})
	// Check to see if a client exists for a username
	CheckIfClientExists(username string) bool
	// Get the room a username is in
	GetUserRoom(username string) string
	// Get the room channel a username is in
	GetUserRoomChannel(username string) string
	// Join a room
	JoinRoom(username, room string)
	// Join a room channel
	JoinRoomChannel(username, channel string)
	// Removes a username from all rooms
	LeaveAllRooms(username string)
	// Checks to see if a username is in a room
	UserInARoom(username string) bool
}

type DataHandler interface {
	// Client connected handler
	NewConnection(ctx *Context)
	// Client disconnect handler
	ConnectionClosed(ctx *Context)
}

type Sockets struct {
	clients       map[string]*Client
	rooms         map[string]*Room
	broadcastChan chan Broadcast
	handler       DataHandler
	sync.Mutex
}

type Context struct {
	*Connection
	*Client
}

type Broadcast struct {
	message *Message
	context *Context
}

type Room struct {
	Name string
	Channel string
}

type Response struct {
	EventName string `json:"eventName"`
	Data interface{} `json:"data"`
}

type Message struct {
	EventName string          `json:"eventName"`
	Data      json.RawMessage `json:"data"`
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
	sockets.rooms = make(map[string]*Room)
	sockets.broadcastChan = make(chan Broadcast)
	sockets.handler = handler
	// Set the JWT token key
	JWTKey = jwtKey
	// Setup go routine to handle messages
	go sockets.HandleMessages()
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

func (s *Sockets) HandleMessages() {
	for {
		brd := <-s.broadcastChan
		EventHandler(brd.message, brd.context)
	}
}

func (s *Sockets) HandleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	query := r.URL.Query()
	jwtString := query.Get("jwt")
	// Check to see if the JWT is undefined
	if jwtString == "undefined" || jwtString == "" {
		ws.Close()
		return
	}
	success, jwt := decodeJWT(jwtString)
	// Unable to decode JWT
	if !success {
		ws.Close()
		return
	}
	var client *Client
	uuid := xid.New().String()
	if s.CheckIfClientExists(jwt.Username) {
		client = s.clients[jwt.Username]
		client.addConnection(&Connection{
			Conn: ws,
			UUID: uuid,
		})
	} else {
		client = &Client{}
		go client.pingHandler()
		client.addConnection(&Connection{
			Conn: ws,
			UUID: uuid,
		})
		client.Username = jwt.Username
		client.connected = true
		s.addClient(client)
	}
	context := &Context{
		Connection: &Connection{
			UUID: uuid,
			Conn: ws,
		},
		Client: client,
	}
	s.handler.NewConnection(context)
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			s.closeWS(s.clients[jwt.Username], ws)
			break
		}
		brd := Broadcast{
			message: &msg,
			context: context,
		}
		s.broadcastChan <- brd
	}

}

func (s *Sockets) BroadcastToRoom(roomName, event string, data, options interface{}) {
	for user, room := range s.rooms {
		if room.Name == roomName {
			var response Response
			response.EventName = event
			response.Data = data
			userClient := s.clients[user]
			for _, conn := range userClient.connections {
				if conn.Conn == nil {
					continue
				}
				conn.Conn.WriteJSON(response)
			}
		}
	}
}

func (s *Sockets) BroadcastToRoomChannel(roomName, channelName, event string, data, options interface{}) {
	for user, room := range s.rooms {
		if room.Name == roomName && room.Channel == channelName {
			var response Response
			response.EventName = event
			response.Data = data
			userClient := s.clients[user]
			for _, conn := range userClient.connections {
				if conn.Conn == nil {
					continue
				}
				conn.Conn.WriteJSON(response)
			}
		}
	}
}

func (s *Sockets) CheckIfClientExists(username string) bool {
	// PLEASE
	if s.clients[username] != nil {
		return true
	}
	return false
}

func (s *Sockets) GetUserRoom(username string) (string) {
	if s.rooms[username] == nil {
		return ""
	}
	return s.rooms[username].Name
}

func (s *Sockets) LeaveAllRooms(username string) {
	s.Lock()
	delete(s.rooms, username)
	s.Unlock()
}

func (s *Sockets) GetUserRoomChannel(username string) (string) {
	if s.rooms[username] == nil {
		return ""
	}
	return s.rooms[username].Channel
}

func (s *Sockets) JoinRoom(username, room string) {
	s.Lock()
	s.rooms[username] = &Room{
		Name: room,
	}
	s.Unlock()
}

func (s *Sockets) JoinRoomChannel(username, channel string) {
	s.Lock()
	if s.rooms[username] == nil {
		return
	}
	s.rooms[username].Channel = channel
	s.Unlock()
}

func (s *Sockets) UserInARoom(username string) (bool) {
	if s.rooms[username] == nil {
		return false
	}
	if s.rooms[username].Name == "" {
		return false
	}
	return true
}

func (s *Sockets) closeWS(client *Client, connection *websocket.Conn) {
	// If we have no client details just close or we are going to have issues.
	if client == nil {
		connection.Close()
	}
	// Call the event listener with the approp. details
	s.handler.ConnectionClosed(&Context{
		Connection: client.getConnection(connection),
		Client:     client,
	})
	// Remove our connection from the user connection list.
	client.removeConnection(connection)
	// Determine whether we need to remove the user from the online list
	if len(client.connections) <= 0 {
		// Remove our client from all the rooms
		s.LeaveAllRooms(client.Username)
		// Remove from local online list
		s.removeClient(client)
	}
	// Close the Connection
	connection.Close()
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

