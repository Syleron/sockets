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

package client

import (
	"crypto/tls"
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/common"
	"net/url"
	"sync"
)

type DataHandler interface {
	// NewConnection Client connected handler
	NewConnection()
	// ConnectionClosed Client disconnect handler
	ConnectionClosed()
	// NewClientError Client error handler
	NewClientError(err error)
}

type Client struct {
	Status   bool `json:"status"`
	ws       *websocket.Conn
	emitChan chan *common.Message
	handler  DataHandler
	sync.Mutex
}

type Secure struct {
	EnableTLS bool
	TLSConfig *tls.Config
}

func Dial(addr string, secure *Secure, handler DataHandler) (*Client, error) {
	client := &Client{}
	client.emitChan = make(chan *common.Message)
	client.handler = handler
	if err := client.New(addr, secure); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) New(addr string, secure *Secure) error {
	var err error
	var sc string

	s := websocket.DefaultDialer
	if secure.EnableTLS {
		s.TLSClientConfig = secure.TLSConfig
		sc = "wss"
	} else {
		sc = "ws"
	}

	u := url.URL{Scheme: sc, Host: addr, Path: "/ws"}
	c.ws, _, err = s.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	// Set our connection status
	c.Status = true
	// Call interface function to signify a new connection
	c.handler.NewConnection()
	// Handle incoming requests
	go c.handleIncoming()
	// Handle outgoing messages
	go c.handleOutgoing()
	return nil
}

func (c *Client) handleIncoming() {
	defer func() {
		c.Close()
	}()
	for {
		var msg common.Message
		err := c.ws.ReadJSON(&msg)
		if err != nil {
			break
		}
		EventHandler(&msg)
	}
}

func (c *Client) handleOutgoing() {
	for {
		select {
		// Take our message from our message channel
		case message := <-c.emitChan:
			//c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteJSON(message); err != nil {
				c.handler.NewClientError(err)
			}
		}
	}
}

func (c *Client) Emit(msg *common.Message) {
	c.emitChan <- msg
}

func (c *Client) Close() {
	// Set our connection status
	c.Status = false
	// Close our connection
	c.ws.Close()
	// Inform our handler
	c.handler.ConnectionClosed()
}

func (c *Client) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}

func (c *Client) IsConnected() bool {
	return c.Status
}
