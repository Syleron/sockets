# Sockets
Sockets package wrapped around gorilla websocket w/ some influence by socket.io

Project is still actively being worked on in other personal projects.

### Features

* JWT authentication.
* Room & Room Channel support.
* Multi-connection under the same auth username.

### Installation

    go get github.com/Syleron/sockets
    
### Simple server usage

    package main

    import (
        "github.com/gin-gonic/gin"
        "github.com/Syleron/sockets"
    )

    func main() {
        sockets := sockets.NewSocket("")

        // Register our events
        sockets.HandleEvent("ping", testing)

        // Setup router
        router := gin.Default()

        // Setup websockets
        router.GET("/ws", func(c *gin.Context) {
            sockets.HandleConnections(c.Writer, c.Request)
        })

        // Start server
        router.Run(":5000")
    }

    func testing(msg *sockets.Message, ctx *sockets.Context) {
        ctx.emit('pong')
    }

### Licence

MIT
