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

    go get github.com/syleron/sockets/v2

Requires Go 1.21 or later. v2 is a breaking release; see [CHANGELOG.md](CHANGELOG.md)
for migration steps from v1.

### Logging

The library does not write to the stdlib `log` package or to stdout. It logs
structured `log/slog` records to an optional logger:

    // Server
    s := sockets.New(handler, &sockets.Config{
        Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
    })

    // Client
    c, err := sktsClient.Dial(addr, "/ws", &sktsClient.Secure{Logger: myLogger}, handler)

When `Logger` is nil, records go to `slog.Default()` (looked up each time, so
`slog.SetDefault` after `New`/`Dial` is honoured). Failures are logged at
Warn, shutdown at Info, and connection/room/session lifecycle messages at
Debug. To silence the library, pass a logger whose handler discards records.

`common.DecodeJWT` never logs. Use `common.ParseJWT` to get the rejection
reason as an error (`errors.Is` with `common.ErrEmptyKey`,
`common.ErrInvalidToken`, `common.ErrMissingUsername` or the `jwt/v5` errors).

### Simple client usage

    package main

    import (
        "fmt"
        sktsClient "github.com/syleron/sockets/v2/client"
        "github.com/syleron/sockets/v2/common"
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
        client, err := sktsClient.Dial("127.0.0.1:5000", "/ws", nil, &SocketHandler{})
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
        "net/http"
        "time"

        "github.com/syleron/sockets/v2"
        "github.com/syleron/sockets/v2/common"
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
        ws := sockets.New(&SocketHandler{}, &sockets.Config{})

        // Register our events
        ws.HandleEvent("ping", ping, false)

        // Setup websockets
        mux := http.NewServeMux()
        mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
            _ = ws.HandleConnection(w, r, "")
        })

        fmt.Println("> Sockets server started. Waiting for connections..")

        // Start server
        srv := &http.Server{Addr: ":5000", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
        panic(srv.ListenAndServe())
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
