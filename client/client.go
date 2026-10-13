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
	"github.com/syleron/sockets/v2/common"
	"log/slog"
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
	// Status reports whether the client is connected. It is written under an
	// internal lock; use IsConnected to read it without a data race.
	Status   bool `json:"status"`
	emitChan chan *common.Message
	handler  DataHandler
	Data     map[string]interface{}
	sync.Mutex

	// mu guards the connection state below and Status. It is separate from
	// the embedded Mutex, which guards Data, and is never held while a
	// DataHandler callback runs.
	mu      sync.Mutex
	cur     *conn
	gen     uint64 // bumped by Reconnect and Close; aborts an in-flight dial
	addr    string
	path    string
	secure  *Secure // copy of the caller's Secure; nil for plain ws
	dialed  bool    // set by Dial; Reconnect needs the stored params
	closed  bool    // set by Close; Reconnect then returns ErrClosed
	discard bool    // NewForTesting(nil): Emit drops messages
	logger  *slog.Logger

	// reconnectMu serialises Reconnect calls.
	reconnectMu sync.Mutex

	// dispatchMu serialises event dispatch across connections, so a
	// replaced connection's handler never runs alongside the new one's.
	dispatchMu sync.Mutex
}

// conn is one websocket connection of a Client. closed and superseded are
// guarded by Client.mu.
type conn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex // gorilla/websocket allows one concurrent writer
	// closed is set when the connection is closed by Close or by a read
	// failure, superseded when Reconnect replaces it. Either way its reader
	// dispatches no more events, and a reader that finds either flag set
	// when its read fails exits without calling the DataHandler.
	closed     bool
	superseded bool
	readerDone chan struct{} // closed when the reader goroutine returns
}

func newConn(ws *websocket.Conn) *conn {
	return &conn{ws: ws, readerDone: make(chan struct{})}
}

type Secure struct {
	EnableTLS bool
	TLSConfig *tls.Config
	ProxyURL  string
	ProxyUser string
	ProxyPass string
	// Logger receives the client's log records. Optional: when nil (or when
	// Dial is given a nil *Secure), the client logs to slog.Default(),
	// resolved at log time. Read and write failures on the current
	// connection are logged at Warn and also passed to
	// DataHandler.NewClientError. On a connection that Reconnect has
	// replaced, write failures are only logged and read failures are
	// dropped silently (best-effort; see Reconnect).
	Logger *slog.Logger
}

// NewForTesting returns a Client that is not connected; Emit sends each
// message on emitChan instead of a websocket so tests can observe it. It is
// for tests only: Reconnect returns an error and it never dials. With a nil
// emitChan, Emit discards messages.
func NewForTesting(emitChan chan *common.Message) *Client {
	return &Client{
		emitChan: emitChan,
		discard:  emitChan == nil,
		Data:     make(map[string]interface{}),
	}
}

// Dial connects to addr and path ("/ws" when empty). secure and its
// TLSConfig are copied, so changing them afterwards does not affect
// Reconnect.
func Dial(addr, path string, secure *Secure, handler DataHandler) (*Client, error) {
	if handler == nil {
		return nil, errors.New("data handler must not be nil")
	}

	client := &Client{
		handler: handler,
		Data:    make(map[string]interface{}),
		addr:    addr,
		path:    path,
		dialed:  true,
	}
	if secure != nil {
		client.secure = copySecure(secure)
		client.logger = secure.Logger
	}

	ws, err := dial(addr, path, client.secure)
	if err != nil {
		return nil, fmt.Errorf("failed to establish connection: %w", err)
	}

	cn := newConn(ws)
	client.mu.Lock()
	client.cur = cn
	client.Status = true
	client.mu.Unlock()

	client.start(cn)
	return client, nil
}

// copySecure returns a copy of secure with its own TLSConfig.
func copySecure(secure *Secure) *Secure {
	s := *secure
	if s.TLSConfig != nil {
		s.TLSConfig = s.TLSConfig.Clone()
	}
	return &s
}

// start announces a new connection and starts its reader. No lock may be
// held: NewConnection may call Reconnect.
func (c *Client) start(cn *conn) {
	c.handler.NewConnection()
	go c.handleIncoming(cn)
}

// dial opens a websocket connection. It configures a copy of
// websocket.DefaultDialer, so settings never leak between dials.
func dial(addr, path string, secure *Secure) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	scheme := "ws"

	if secure != nil {
		if err := configureDialer(&dialer, secure); err != nil {
			return nil, err
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
	return ws, err
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

// Reconnect closes the current connection and dials again with the
// parameters given to Dial (or to the last ReconnectWithSecure).
//
// ConnectionClosed is not called for the replaced connection; NewConnection
// is called for the new one. Handlers registered with HandleEvent survive.
// Reconnect is safe to call from inside a DataHandler callback (including
// ConnectionClosed and NewConnection) or an event handler. It is safe to
// call concurrently with Emit, Close and other Reconnect calls; Reconnect and
// ReconnectWithSecure calls are serialised with each other, but not with
// Close or Emit. Messages emitted while the old connection is torn down are
// dropped and logged, not delivered. Event handlers never run for two
// connections at once, so an event handler that calls Reconnect and then
// waits for an event from the new connection deadlocks.
//
// Close is final: after it, Reconnect returns ErrClosed. If Close is called
// while the dial is in flight, Reconnect closes the new connection and
// returns ErrClosed. A retry loop should stop when
// errors.Is(err, ErrClosed). A connection lost by the server is not a Close,
// so Reconnect from ConnectionClosed works.
//
// A failed Reconnect does not call ConnectionClosed (the old connection was
// replaced deliberately), so the caller owns the retry. Until a later
// Reconnect succeeds the client stays disconnected and Emit drops messages
// (logged at Warn). A Close in that state, or while a Reconnect is dialling,
// does not call ConnectionClosed either: there is no live connection to
// close.
//
// Suppression of the replaced connection's callbacks is best-effort when
// Reconnect runs on another goroutine. Its reader checks that the
// connection is still current and then calls NewClientError or
// ConnectionClosed without holding a lock, so a Reconnect that completes
// between the check and the call can still see one late callback for the
// replaced connection. A Reconnect from inside those callbacks is not
// affected. Likewise, if Close races with the end of a successful Reconnect,
// ConnectionClosed may be delivered before that connection's NewConnection,
// and when two Reconnects run back to back on different goroutines their
// NewConnection calls may arrive in either order.
func (c *Client) Reconnect() error {
	c.reconnectMu.Lock()
	cn, err := c.reconnectLocked()
	c.reconnectMu.Unlock()
	if err != nil {
		return err
	}
	c.start(cn)
	return nil
}

// checkReconnect returns why the client cannot reconnect, if it cannot. The
// caller holds c.mu.
func (c *Client) checkReconnect() error {
	if !c.dialed {
		return errNotDialed
	}
	if c.closed {
		return ErrClosed
	}
	return nil
}

// reconnectLocked replaces the current connection with a new one, which the
// caller must start. The caller holds c.reconnectMu.
func (c *Client) reconnectLocked() (*conn, error) {
	c.mu.Lock()
	if err := c.checkReconnect(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.gen++
	gen := c.gen
	old := c.cur
	if old != nil {
		old.superseded = true
	}
	c.Status = false
	addr, path, secure := c.addr, c.path, c.secure
	c.mu.Unlock()

	if old != nil {
		// The old connection may already be closed (by a read failure or
		// an earlier Reconnect), so a failure here is expected.
		if err := old.ws.Close(); err != nil {
			c.log().Debug("closing replaced websocket connection", slog.Any("error", err))
		}
	}

	ws, err := dial(addr, path, secure)
	if err != nil {
		return nil, fmt.Errorf("failed to re-establish connection: %w", err)
	}

	c.mu.Lock()
	if c.gen != gen {
		// Only Close bumps gen while reconnectMu is held.
		c.mu.Unlock()
		_ = ws.Close()
		return nil, ErrClosed
	}
	cn := newConn(ws)
	c.cur = cn
	c.Status = true
	c.mu.Unlock()

	return cn, nil
}

// ReconnectWithSecure is Reconnect with new security settings (for example
// after a certificate change). secure and its TLSConfig are copied and replace
// the stored settings completely, including Logger. secure must not be nil:
// use &Secure{} for plain ws.
//
// Settings that are invalid before any dial (a nil secure, an unparsable
// ProxyURL) are rejected up front: the error is returned and the current
// connection and stored settings are left untouched. Otherwise the settings
// are stored even if the dial fails, so a later Reconnect retries with them.
// As with Reconnect, a failed dial does not call ConnectionClosed; the caller
// owns the retry.
func (c *Client) ReconnectWithSecure(secure *Secure) error {
	if secure == nil {
		return errNilSecure
	}
	s := copySecure(secure)
	// Validate on a scratch dialer before anything is stored or closed.
	var scratch websocket.Dialer
	if err := configureDialer(&scratch, s); err != nil {
		return err
	}

	c.reconnectMu.Lock()
	c.mu.Lock()
	if err := c.checkReconnect(); err != nil {
		c.mu.Unlock()
		c.reconnectMu.Unlock()
		return err
	}
	c.secure = s
	c.logger = s.Logger
	c.mu.Unlock()
	cn, err := c.reconnectLocked()
	c.reconnectMu.Unlock()
	if err != nil {
		return err
	}
	c.start(cn)
	return nil
}

// ErrClosed is returned by Reconnect and ReconnectWithSecure after Close.
var ErrClosed = errors.New("client: closed")

var (
	errNotDialed = errors.New("client: Reconnect requires a client created by Dial")
	errNilSecure = errors.New("client: ReconnectWithSecure needs a non-nil *Secure; use &Secure{} for plain ws")
)

func (c *Client) handleIncoming(cn *conn) {
	defer close(cn.readerDone)
	for {
		var msg common.Message
		err := cn.ws.ReadJSON(&msg)
		if err != nil {
			c.mu.Lock()
			report := c.cur == cn && !cn.closed && !cn.superseded
			if report {
				cn.closed = true
				c.Status = false
			}
			c.mu.Unlock()

			if closeErr := cn.ws.Close(); closeErr != nil && report {
				c.log().Warn("failed to close websocket connection", slog.Any("error", closeErr))
			}
			// Superseded or closed connections exit silently.
			if !report {
				return
			}
			// A Reconnect (for example from a callback) may have replaced
			// the connection meanwhile; then the new one is not closed.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.log().Warn("websocket read failed", slog.Any("error", err))
				if !c.isCurrent(cn) {
					return
				}
				c.handler.NewClientError(err)
			}
			if !c.isCurrent(cn) {
				return
			}
			c.handler.ConnectionClosed()
			return
		}

		if !c.dispatch(cn, &msg) {
			return
		}
	}
}

func (c *Client) isCurrent(cn *conn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur == cn
}

// dispatch passes msg to its event handler unless cn has been closed or
// replaced, which it reports by returning false. dispatchMu keeps handlers
// for different connections from running at once.
func (c *Client) dispatch(cn *conn, msg *common.Message) bool {
	c.dispatchMu.Lock()
	defer c.dispatchMu.Unlock()

	c.mu.Lock()
	stale := cn.closed || cn.superseded
	c.mu.Unlock()
	if stale {
		return false
	}
	EventHandler(msg)
	return true
}

// Emit writes msg to the connection. A write failure is logged and passed to
// DataHandler.NewClientError on the caller's goroutine, unless Reconnect has
// replaced the connection, in which case the message is dropped and logged.
func (c *Client) Emit(msg *common.Message) {
	c.mu.Lock()
	cn := c.cur
	c.mu.Unlock()

	if cn == nil {
		// NewForTesting client. A zero-value Client blocks on its nil
		// channel, as in v2.2.0.
		if !c.discard {
			c.emitChan <- msg
		}
		return
	}

	cn.writeMu.Lock()
	err := cn.ws.WriteJSON(msg)
	cn.writeMu.Unlock()
	if err == nil {
		return
	}

	c.mu.Lock()
	superseded := cn.superseded
	c.mu.Unlock()
	if superseded {
		c.log().Warn("dropping message: not connected (connection replaced by Reconnect)", slog.Any("error", err))
		return
	}
	c.log().Warn("failed to send message", slog.Any("error", err))
	c.handler.NewClientError(err)
}

// Close closes the connection and calls DataHandler.ConnectionClosed, once
// per connection: closing an already closed or replaced connection does not
// call it again. In particular, Close after a failed Reconnect, or while a
// Reconnect is dialling, does not call ConnectionClosed. Close is final: it
// aborts an in-flight Reconnect, and later calls to Reconnect and
// ReconnectWithSecure return ErrClosed. ConnectionClosed runs synchronously
// on the goroutine that calls Close.
func (c *Client) Close() {
	c.mu.Lock()
	c.gen++
	c.closed = true
	cn := c.cur
	fire := cn != nil && !cn.closed && !cn.superseded
	if cn != nil {
		cn.closed = true
	}
	c.Status = false
	c.mu.Unlock()

	if cn != nil {
		// A connection that was already closed or replaced fails here as
		// expected.
		if err := cn.ws.Close(); err != nil {
			if fire {
				c.log().Warn("failed to close websocket connection", slog.Any("error", err))
			} else {
				c.log().Debug("closing websocket connection", slog.Any("error", err))
			}
		}
	}

	if fire {
		c.handler.ConnectionClosed()
	}
}

// log returns the logger from Secure, or slog.Default() when unset.
func (c *Client) log() *slog.Logger {
	c.mu.Lock()
	logger := c.logger
	c.mu.Unlock()
	if logger != nil {
		return logger
	}
	return slog.Default()
}

func (c *Client) HandleEvent(pattern string, handler EventFunc) {
	if events == nil {
		events = make(map[string]EventFunc)
	}
	events[pattern] = handler
}

// IsConnected reports whether the client is connected. Unlike reading Status
// directly, it is safe to call concurrently with the client's goroutines.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
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
