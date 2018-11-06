```
   ____         __       __    
  / __/__  ____/ /_____ / /____
 _\ \/ _ \/ __/  '_/ -_) __(_-<
/___/\___/\__/_/\_\\__/\__/___/               

```
[![Build Status](https://travis-ci.org/Syleron/sockets.svg?branch=master)](https://travis-ci.org/Syleron/sockets)
<a href="https://godoc.org/github.com/Syleron/sockets"><img src="https://godoc.org/github.com/Syleron/sockets?status.svg"><a/>
<a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/github/license/mashape/apistatus.svg"><a/>

Sockets is a websocket framework based on gorilla/websocket providing a simple way to write real-time apps.

### Features

* JWT authentication.
* Room & Room Channel support.
* Easily broadcast to Rooms/Channels.
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
