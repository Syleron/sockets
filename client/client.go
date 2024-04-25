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

package client

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/common"
	"log"
	"net/http"
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
	Data     map[string]interface{}
	sync.Mutex
}

type Secure struct {
	EnableTLS bool
	TLSConfig *tls.Config
	ProxyURL  string
	ProxyUser string
	ProxyPass string
}

func Dial(addr, path string, secure *Secure, handler DataHandler) (*Client, error) {
	if handler == nil {
		return nil, errors.New("data handler must not be nil")
	}

	client := &Client{
		emitChan: make(chan *common.Message),
		handler:  handler,
		Data:     make(map[string]interface{}),
	}

	if err := client.connect(addr, path, secure); err != nil {
		return nil, fmt.Errorf("failed to establish connection: %w", err)
	}

	return client, nil
}

func (c *Client) connect(addr, path string, secure *Secure) error {
	dialer := websocket.DefaultDialer
	scheme := "ws"

	if secure != nil {
		if err := configureDialer(dialer, secure); err != nil {
			return err
		}
		if secure.EnableTLS {
			scheme = "wss"
		}
	}

	if path == "" {
		path = "/ws"
	}

	url := url.URL{Scheme: scheme, Host: addr, Path: path}
	ws, _, err := dialer.Dial(url.String(), nil)
	if err != nil {
		return err
	}

	c.ws = ws
	c.Status = true
	c.handler.NewConnection()

	go c.handleIncoming()
	go c.handleOutgoing()

	return nil
}

func configureDialer(dialer *websocket.Dialer, secure *Secure) error {
	if secure.EnableTLS {
		dialer.TLSClientConfig = secure.TLSConfig
	}

	if secure.ProxyURL != "" {
		proxyURL, err := url.Parse(secure.ProxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		dialer.Proxy = http.ProxyURL(proxyURL)
		if secure.ProxyUser != "" && secure.ProxyPass != "" {
			proxyURL.User = url.UserPassword(secure.ProxyUser, secure.ProxyPass)
		}
	}

	return nil
}

func (c *Client) handleIncoming() {
	defer c.Close() // Ensure connection is closed after function exits
	for {
		var msg common.Message
		err := c.ws.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Error: %v", err)
				c.handler.NewClientError(err)
			}
			break
		}
		EventHandler(&msg)
	}
}

func (c *Client) handleOutgoing() {
	for message := range c.emitChan { // Will exit loop if channel is closed
		if err := c.ws.WriteJSON(message); err != nil {
			log.Printf("Failed to send message: %v", err)
			c.handler.NewClientError(err)
			continue
		}
	}
}

func (c *Client) Emit(msg *common.Message) {
	c.emitChan <- msg
}

func (c *Client) Close() {
	c.Status = false

	if c.ws != nil {
		if err := c.ws.Close(); err != nil {
			log.Printf("Error closing WebSocket connection: %v", err)
		}
	}

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

func (c *Client) SetData(key string, value interface{}) {
	c.Lock()
	defer c.Unlock()
	c.Data[key] = value
}

func (c *Client) GetData(key string) interface{} {
	c.Lock()
	defer c.Unlock()
	return c.Data[key]
}

func (c *Client) DeleteData(key string) {
	c.Lock()
	defer c.Unlock()
	delete(c.Data, key)
}
