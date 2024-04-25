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
	"log"
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
	// Setting up a secure configuration, even if TLS is not used.
	secureConfig := &sktsClient.Secure{
		EnableTLS: false, // Set to `true` and configure TLSConfig to use TLS.
	}

	// Create our websocket client
	client, err := sktsClient.Dial("127.0.0.1:9443", "/ws", secureConfig, &SocketHandler{})
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}

	// Register event handler for 'pong' messages
	client.HandleEvent("pong", pong)

	// Send our initial 'ping' request
	client.Emit(&common.Message{
		EventName: "ping",
	})

	// Example of sending messages at intervals
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	count := 0
	for range ticker.C {
		if count < 1 {
			fmt.Println("Sending 'ping' message")
			client.Emit(&common.Message{
				EventName: "ping",
			})
			count++
		} else {
			client.Close() // Properly close the connection
			break
		}
	}
}

func pong(msg *common.Message) {
	fmt.Println("> Recieved WSKT 'pong'")
}
