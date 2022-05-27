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

package main

import (
	"fmt"
	sktsClient "github.com/syleron/sockets/client"
	"github.com/syleron/sockets/common"
	"time"
)

type SocketHandler struct{}

func (h *SocketHandler) NewConnection() {
	// Do something when a new connection comes in
	fmt.Println("> Connection established")
}

func (h *SocketHandler) ConnectionClosed() {
	// Do something when a connection is closed
	fmt.Println("> Connection closed")
}

func (h *SocketHandler) NewClientError(err error) {
	// Do something when a connection error is found
	fmt.Printf("> DEBUG %v\n", err)
}

func main() {
	// Create our websocket client
	client, err := sktsClient.Dial("127.0.0.1:9443", &sktsClient.Secure{
		EnableTLS: false,
		TLSConfig: nil,
	}, &SocketHandler{})

	// Handle any errors
	if err != nil {
		panic(err)
	}

	// Define event handler
	client.HandleEvent("pong", pong)

	payload := &common.Message{
		EventName: "ping",
	}

	// Send our initial request
	client.Emit(payload)

	// Send another
	count := 0
	for range time.Tick(5 * time.Second) {
		if count < 1 {
			client.Emit(payload)
			count++
		} else {
			return
		}
	}
}

func pong(msg *common.Message) {
	fmt.Println("> Recieved WSKT 'pong'")
}
