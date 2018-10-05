# Sockets
Sockets package wrapped around gorilla websocket w/ some influence by socket.io

Project is still actively being worked on in other personal projects.

STATUS: Not ready for use. Still making some minor changes.

### Features

* JWT authentication.
* Room & Room Channel support.
* Multi-connection under the same auth username.

### Installation

    go get github.com/Syleron/sockets
    
### Simple server usage

    package main

    import skt (
        "github.com/gin-gonic/gin"
        "gitlab.com/syleron/sockets"
    )

    func main() {
        sockets := skt.NewSocket("")

        // Register our events
        sockets.HandleEvent("ping", testing)

        // setup router
        router := gin.Default()

        // Setup websockets
        router.GET("/ws", func(c *gin.Context) {
            sockets.HandleConnections(c.Writer, c.Request)
        })

        // Start server
        router.Run(":5000")
    }

    func testing(msg skt.Message) {
    }

### Licence

MIT
