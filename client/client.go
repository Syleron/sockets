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
	"github.com/Syleron/sockets/common"
	"github.com/gorilla/websocket"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"
)

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

type DataHandler interface {
	// Client connected handler
	NewConnection()
	// Client disconnect handler
	ConnectionClosed()
	// Client error handler
	NewClientError(err error)
}

type Client struct {
	conn *websocket.Conn
	emitChan chan *common.Message
	interrupt chan os.Signal
	done chan struct{}
	handler DataHandler
	sync.Mutex
}

func Dial(addr, jwt string, secure bool, handler DataHandler) (*Client, error) {
	client := &Client{}
	client.emitChan = make(chan *common.Message)
	client.interrupt = make(chan os.Signal, 1)
	client.done = make(chan struct{})
	client.handler = handler
	signal.Notify(client.interrupt, os.Interrupt)
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
	c.conn, _, err = websocket.DefaultDialer.Dial(u.String() + "?jwt="+jwt, nil)
	if err != nil {
		return err
	}
	c.handler.NewConnection()
	// Handle incoming requests
	go c.handleIncoming()
	// Handle messages
	go c.handleMessages()
	return nil
}

func (c *Client) handleMessages() {
	for {
		select {
		case <-c.done:
			return
		case d := <-c.emitChan:
			if err := c.conn.WriteJSON(d); err != nil {
				c.handler.NewClientError(err)
			}
		case <-c.interrupt:
			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				c.handler.NewClientError(err)
				return
			}
		}
	}
}

func (c *Client) handleIncoming() {
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		select {
		case <-c.done:
			return
		default:
			var msg common.Message
			err := c.conn.ReadJSON(&msg)
			if err != nil {
				c.handler.NewClientError(err)
				break;
			}
			EventHandler(&msg)
		}
	}
}

func (c *Client) Emit(msg *common.Message) {
	c.emitChan <- msg
}

func (c *Client) Close() {
	// Close our channels
	close(c.done)
	// Close our connection
	c.conn.Close();
	// Inform our handler
	c.handler.ConnectionClosed()
}

func (c *Client) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}
