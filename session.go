// MIT License
//
// Copyright (c) 2022 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
/// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
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
	"github.com/syleron/sockets/common"
	"sync"
)

type Session struct {
	Username    string
	connections map[string]*Connection
	sync.Mutex
}

func (s *Session) HasSession() bool {
	return s.Username != "" && len(s.connections) > 0
}

func (s *Session) Emit(msg *common.Message) {
	for _, connection := range s.connections {
		if err := connection.Emit(msg); err != nil {
			continue
		}
	}
}

func (s *Session) addConnection(newConnection *Connection) {
	s.Lock()
	defer s.Unlock()
	if s.connections == nil {
		s.connections = make(map[string]*Connection)
	}
	// Append our connection
	s.connections[newConnection.UUID] = newConnection
}

func (s *Session) removeConnection(uuid string) {
	s.Lock()
	defer s.Unlock()
	// Remove our connection from our connections array
	delete(s.connections, uuid)
}
