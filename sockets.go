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
	// Remove a client by username
	RemoveClientByUsername(username string)
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

type Sockets struct {
	clients       map[string]*Client
	rooms         map[string]*Room
	broadcastChan chan Broadcast
}

type Context struct {
	*Client
	Conn *websocket.Conn
}

type Broadcast struct {
	message *Message
	context *Context
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func New(jwtKey string) *Sockets {
	// Setup the sockets object
	sockets := &Sockets{}
	sockets.clients = make(map[string]*Client)
	sockets.rooms = make(map[string]*Room)
	sockets.broadcastChan = make(chan Broadcast)
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
	if jwtString == "undefined" || jwtString == ""  {
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
	if s.CheckIfClientExists(jwt.Username) {
		client = s.clients[jwt.Username]
		client.Connections = append(client.Connections, ws)
	} else {
		client = &Client{}
		go client.PingHandler()
		client.Connections = append(client.Connections, ws)
		client.Username = jwt.Username
		client.connected = true
		s.addClient(client)
	}
	for {
		var msg Message
		err := ws.ReadJSON(&msg)
		if err != nil {
			s.closeWS(s.clients[jwt.Username], ws)
			break
		}
		msg.Username = jwt.Username
		brd := Broadcast{
			message: &msg,
			context: &Context{
				Client: client,
				Conn: ws,
			},
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
			for _, conn := range userClient.Connections {
				err := conn.WriteJSON(response)
				if err != nil {
					s.closeWS(userClient, conn)
				}
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
			for _, conn := range userClient.Connections {
				err := conn.WriteJSON(response)
				if err != nil {
					s.closeWS(userClient, conn)
				}
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

func (s *Sockets) RemoveClientByConnection(conn *websocket.Conn) {
	for username, client := range s.clients {
		for _, c := range client.Connections {
			if conn == c {
				s.RemoveClientByUsername(username)
			}
		}
	}
}

func (s *Sockets) RemoveClientByUsername(username string) {
	s.LeaveAllRooms(username)
	delete(s.clients, username)
}

func (s *Sockets) GetUserRoom(username string) (string) {
	if s.rooms[username] == nil {
		return ""
	}
	return s.rooms[username].Name
}

func (s *Sockets) LeaveAllRooms(username string) {
	delete(s.rooms, username)
}

func (s *Sockets) GetUserRoomChannel(username string) (string) {
	if s.rooms[username] == nil {
		return ""
	}
	return s.rooms[username].Channel
}

func (s *Sockets) JoinRoom(username, room string) {
	s.rooms[username] = &Room{
		Name: room,
	}
}

func (s *Sockets) JoinRoomChannel(username, channel string) {
	if s.rooms[username] == nil {
		return
	}
	s.rooms[username].Channel = channel
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
	// Remove our connection from the user connection list.
	client.RemoveConnection(connection)
	// Determine whether we need to remove the user from the online list
	if len(s.clients[client.Username].Connections) <= 0 {
		// Remove from local online list
		s.RemoveClientByUsername(client.Username)
	}
	// Close the Connection
	connection.Close()
}

func (s *Sockets) addClient(client *Client) {
	s.clients[client.Username] = client
}

