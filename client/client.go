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
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"
)

type DataHandler interface {
	// Client connected handler
	NewConnection()
	// Client disconnect handler
	ConnectionClosed()
}

type Client struct {
	conn *websocket.Conn
	emitChan chan *common.Message
	interrupt chan os.Signal
	done chan struct{}
	sync.Mutex
}

func Dial(addr, jwt string, secure bool) *Client {
	client := &Client{}
	client.emitChan = make(chan *common.Message)
	client.interrupt = make(chan os.Signal, 1)
	client.done = make(chan struct{})
	signal.Notify(client.interrupt, os.Interrupt)
	client.New(addr, jwt, secure)
	return client
}

func (c *Client) New(addr, jwt string, secure bool) {
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
		log.Fatal("dial:", err)
	}

	// Handle incoming requests
	go c.handleIncoming()

	// Handle messages
	go c.handleMessages()
}

func (c *Client) handleMessages() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case d := <-c.emitChan:
			if err := c.conn.WriteJSON(d); err != nil {
				log.Println("write:", err)
			}
		case <-c.interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-c.done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func (c *Client) handleIncoming() {
	defer close(c.done)
	for {
		var msg common.Message
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			log.Println("read:", err)
			continue
		}
		EventHandler(&msg)
	}
}

func (c *Client) Emit(msg *common.Message) {
	c.emitChan <- msg
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}
