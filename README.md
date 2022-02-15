```
   ____         __       __    
  / __/__  ____/ /_____ / /____
 _\ \/ _ \/ __/  '_/ -_) __(_-<
/___/\___/\__/_/\_\\__/\__/___/               

```
[![Build Status](https://travis-ci.org/syleron/sockets.svg?branch=master)](https://travis-ci.org/syleron/sockets)
<a href="https://godoc.org/github.com/syleron/sockets"><img src="https://godoc.org/github.com/syleron/sockets?status.svg"><a/>
<a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/github/license/mashape/apistatus.svg"><a/>

Sockets is a websocket framework based on gorilla/websocket providing a simple way to write real-time apps.

### Features

* Room & Room Channel support.
* Easily broadcast to Rooms/Channels.
* Multiple connections under the same username.

### Installation

    go get github.com/syleron/sockets

### Simple client usage

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
        client, err := sktsClient.Dial("127.0.0.1:5000", false, &SocketHandler{})
        if err != nil {
            panic(err)
        }
        defer client.Close()

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

### Simple server usage

    package main

    import (
        "fmt"
        "github.com/syleron/sockets"
        "github.com/syleron/sockets/common"
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
        sockets := sockets.New(&SocketHandler{})

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

### Projects using sockets

- Yudofu: Anime social network

Note: If your project is not listed here, let us know! :)

### Licence

MIT
