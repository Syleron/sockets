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
	"github.com/syleron/sockets/v2/common"
	"sync"
)

type Session struct {
	Username    string
	connections map[string]*Connection
	sync.Mutex
}

func (s *Session) HasSession() bool {
	s.Lock()
	defer s.Unlock()
	return s.Username != "" && len(s.connections) > 0
}

// Emit writes msg to every connection in the session. The connection set is
// copied under the session lock and the writes happen after it is released,
// so a slow connection does not block session changes, and the connections
// map is never read while another goroutine adds or removes a connection.
func (s *Session) Emit(msg *common.Message) {
	s.Lock()
	conns := make([]*Connection, 0, len(s.connections))
	for _, connection := range s.connections {
		conns = append(conns, connection)
	}
	s.Unlock()

	for _, connection := range conns {
		// Best effort, as before: a failed write to one connection does not
		// stop delivery to the others.
		_ = connection.Emit(msg)
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
