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
package main

import (
	"fmt"
	"github.com/Syleron/sockets"
	"github.com/Syleron/sockets/common"
	"github.com/gin-gonic/gin"
)

type SocketHandler struct {}

func (h *SocketHandler) NewConnection(ctx *sockets.Context) {
	// Do something when a new connection comes in
	fmt.Println("> Connection established")
}

func (h *SocketHandler) ConnectionClosed(ctx *sockets.Context) {
	// Do something when a connection is closed
	fmt.Println("> Connection closed")
}

func main () {
	// Setup socket server
	sockets := sockets.New("SuperSecretKey", &SocketHandler{})

	// Register our events
	sockets.HandleEvent("ping", ping)

	// Set our gin release mode
	gin.SetMode(gin.ReleaseMode)

	// Setup router
	router := gin.Default()

	// Setup websockets
	router.GET("/ws", func(c *gin.Context) {
		sockets.HandleConnection(c.Writer, c.Request)
	})

	fmt.Println("> Sockets server started. Waiting for connections..")

	// Start server
	router.Run(":5000")
}

func ping(msg *common.Message, ctx *sockets.Context) {
	fmt.Println("> Recieved WSKT 'ping' responding with 'pong'")
	ctx.Emit(&common.Message{
		EventName: "pong",
	})
}
