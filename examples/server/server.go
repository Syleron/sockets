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
	"github.com/gin-gonic/gin"
	"github.com/syleron/sockets"
	"github.com/syleron/sockets/common"
	"log"
	"time"
)

var ws *sockets.Sockets

type SocketHandler struct{}

func (h *SocketHandler) NewConnection(ctx *sockets.Context) {
	fmt.Println("> Connection established")
}

func (h *SocketHandler) ConnectionClosed(ctx *sockets.Context) {
	fmt.Println("> Connection closed")
}

func (h *SocketHandler) NewClientError(err error) {
	log.Printf("Error: %v", err)
}

func main() {
	config := &sockets.Config{
		WriteWait:     10 * time.Second,
		PongWait:      60 * time.Second,
		PingPeriod:    54 * time.Second, // 90% of PongWait
		ReadLimitSize: 512,
	}
	// Setup socket server with proper configuration
	ws = sockets.New(&SocketHandler{}, config)

	ws.HandleEvent("ping", ping, false)

	// Set our gin release mode
	gin.SetMode(gin.ReleaseMode)

	// Setup router
	router := gin.Default()

	// Setup websockets
	router.GET("/ws", func(c *gin.Context) {
		if err := ws.HandleConnection(c.Writer, c.Request, ""); err != nil {
			return
		}
	})

	fmt.Println("> Sockets server started. Waiting for connections...")

	// Start server
	if err := router.Run(":9443"); err != nil {
		panic(err)
	}
}

func ping(msg *common.Message, ctx *sockets.Context) {
	fmt.Println("> Received WS 'ping', responding with 'pong'")

	// Store our connection for a particular user
	if err := ws.AddSession("user1", ctx.Connection); err != nil {
		return
	}

	ctx.Emit(&common.Message{
		EventName: "pong",
	})
}
