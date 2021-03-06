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
package client

import (
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/common"
	"net/url"
	"sync"
)

type DataHandler interface {
	// Client connected handler
	NewConnection()
	// Client disconnect handler
	ConnectionClosed()
	// Client error handler
	NewClientError(err error)
}

type Client struct {
	ws       *websocket.Conn
	emitChan chan *common.Message
	handler  DataHandler
	sync.Mutex
}

func Dial(addr, jwt string, secure bool, handler DataHandler) (*Client, error) {
	client := &Client{}
	client.emitChan = make(chan *common.Message)
	client.handler = handler
	if err := client.New(addr, jwt, secure); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) New(addr, jwt string, secure bool) error {
	var err error
	var sc string
	if secure {
		sc = "wss"
	} else {
		sc = "ws"
	}
	u := url.URL{Scheme: sc, Host: addr, Path: "/ws"}
	c.ws, _, err = websocket.DefaultDialer.Dial(u.String()+"?jwt="+jwt, nil)
	if err != nil {
		return err
	}
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
