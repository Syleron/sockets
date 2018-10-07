package sockets

import (
	"github.com/gorilla/websocket"
	"net/http"
	"log"
)

type Sockets interface {
	// Close websocket connection
	Close()
	// Function used to define a WS event listener
	HandleEvent(pattern string, handler EventFunc)
	// Handle incoming WS connections via a router
	HandleConnections(w http.ResponseWriter, r *http.Request)
	// Function used to broadcast an event to members defined in a room
	BroadcastToRoom(roomName, event string, data interface{})
	// Function used to broadcast an event to members defined in a room's channel
	BroadcastToRoomChannel(roomName, channelName, event string, data interface{})
	// Check to see if a client exists for a username
	CheckIfClientExists(username string) bool
	// Remove a client by username
	RemoveClientByUsername(username string)
}

type sockets struct {
	clients       map[string]*Client
	rooms         map[string]*Room
	broadcastChan chan Message
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewSocket(jwtKey string) *sockets {
	// Setup the sockets object
	sockets := &sockets{}
	sockets.clients = make(map[string]*Client)
	sockets.rooms = make(map[string]*Room)
	sockets.broadcastChan = make(chan Message)
	// Set the JWT token key
	JWTKey = jwtKey
	// Setup go routine to handle messages
	go sockets.HandleMessages()
	// return our object
	return sockets
}

func (s *sockets) Close() {
	s.Close()
}

func (s *sockets) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}

func (s *sockets) HandleMessages() {
	for {
		msg := <-s.broadcastChan
		EventHandler(msg)
	}
}

func (s *sockets) HandleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	query := r.URL.Query()
	jwtString := query.Get("jwt")
	// Check to see if the JWT is undefined
	var jwt JWT
	if JWTKey != "" {
		if jwtString == "undefined" || jwtString == ""  {
			ws.Close()
			return
		}
		var success bool
		success, jwt = decodeJWT(jwtString)
		// Unable to decode JWT
		if !success {
			ws.Close()
			return
		}
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()
	// TODO: double check to see if we have a client before create a new instance of one.
	if s.CheckIfClientExists(jwt.Username) {
		client := s.clients[jwt.Username]
		client.Connections = append(client.Connections, ws)
		// amend the client object with our connection
	} else {
		//Define new client object
		newClient := Client{}
		newClient.Connections = append(newClient.Connections, ws)
		newClient.Username = jwt.Username
		newClient.Connected = true
		// Register our new client
		s.addClient(&newClient)

	}
	// Handle messages for this socket connection
	for {
		var msg Message
		// Read in a new message as JSON and map it to a Message object
		err := ws.ReadJSON(&msg)
		// catch errors
		if err != nil {
			// remove the connection from our list
			s.closeWS(s.clients[jwt.Username], ws)
			break
		}
		// Set the username for the connection
		msg.Username = jwt.Username
		// Send the newly received message to the broadcast channel
		s.broadcastChan <- msg // This needs to be revised.
	}

}

func (s *sockets) BroadcastToRoom(roomName, event string, data interface{}) {
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

func (s *sockets) BroadcastToRoomChannel(roomName, channelName, event string, data interface{}) {
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

func (s *sockets) CheckIfClientExists(username string) bool {
	// PLEASE
	if s.clients[username] != nil {
		return true
	}
	return false
}

func (s *sockets) RemoveClientByConnection(conn *websocket.Conn) {
	for username, client := range s.clients {
		for _, c := range client.Connections {
			if conn == c {
				s.RemoveClientByUsername(username)
			}
		}
	}
}

func (s *sockets) RemoveClientByUsername(username string) {
	s.leaveAllRooms(username)
	delete(s.clients, username)
}

func (s *sockets) closeWS(client *Client, connection *websocket.Conn) {
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

func (s *sockets) addClient(client *Client) {
	s.clients[client.Username] = client
}

func (s *sockets) getUserRoom(username string) (string) {
	if s.rooms[username] == nil {
		return ""
	}
	return s.rooms[username].Name
}

func (s *sockets) leaveAllRooms(username string) {
	delete(s.rooms, username)
}

func (s *sockets) userInARoom(username string) (bool) {
	if s.rooms[username] == nil {
		return false
	}
	if s.rooms[username].Name == "" {
		return false
	}
	return true
}
