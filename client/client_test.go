package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/v2/common"
)

const waitFor = 5 * time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recHandler is a DataHandler that counts its callbacks.
type recHandler struct {
	newConn, closed, errs atomic.Int64
	// onNewConn, onClosed and onErr, if set, run inside the callbacks
	// (lb_sidecar reconnects from ConnectionClosed). onNewConn gets the
	// NewConnection count including this call.
	onNewConn func(n int64)
	onClosed  func()
	onErr     func()
}

func (h *recHandler) NewConnection() {
	n := h.newConn.Add(1)
	if h.onNewConn != nil {
		h.onNewConn(n)
	}
}

func (h *recHandler) ConnectionClosed() {
	h.closed.Add(1)
	if h.onClosed != nil {
		h.onClosed()
	}
}

func (h *recHandler) NewClientError(error) {
	h.errs.Add(1)
	if h.onErr != nil {
		h.onErr()
	}
}

func (h *recHandler) check(t *testing.T, newConn, closed, errs int64) {
	t.Helper()
	if got := h.newConn.Load(); got != newConn {
		t.Errorf("NewConnection called %d times, want %d", got, newConn)
	}
	if got := h.closed.Load(); got != closed {
		t.Errorf("ConnectionClosed called %d times, want %d", got, closed)
	}
	if got := h.errs.Load(); got != errs {
		t.Errorf("NewClientError called %d times, want %d", got, errs)
	}
}

// serverConn is one accepted server-side connection. seq numbers connections
// in the order their upgrade requests arrived.
type serverConn struct {
	ws   *websocket.Conn
	seq  int64
	done chan struct{} // closed when the server's read loop exits
}

// closeWith sends a close frame with code from the server. The client
// answers it and closes the connection.
func (sc *serverConn) closeWith(code int) error {
	return sc.ws.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, "test"), time.Now().Add(time.Second))
}

type received struct {
	sc  *serverConn
	msg common.Message
}

// testServer accepts websocket connections, hands each one to the test on
// conns and forwards every message other than "noise" on msgs.
type testServer struct {
	srv   *httptest.Server
	addr  string
	conns chan *serverConn
	msgs  chan received
	seq   atomic.Int64
	// beforeUpgrade, if set, runs with the connection's seq before the
	// upgrade.
	beforeUpgrade func(seq int64)
}

func newTestServer(t *testing.T, tlsServer bool) *testServer {
	t.Helper()
	ts := &testServer{conns: make(chan *serverConn, 256), msgs: make(chan received, 64)}
	stop := make(chan struct{})
	upgrader := websocket.Upgrader{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seq := ts.seq.Add(1)
		if ts.beforeUpgrade != nil {
			ts.beforeUpgrade(seq)
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sc := &serverConn{ws: ws, seq: seq, done: make(chan struct{})}
		defer close(sc.done)
		defer func() { _ = ws.Close() }()
		ts.conns <- sc
		for {
			var msg common.Message
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			if msg.EventName == "noise" {
				continue
			}
			select {
			case ts.msgs <- received{sc: sc, msg: msg}:
			case <-stop:
				return
			}
		}
	})
	ts.srv = httptest.NewUnstartedServer(h)
	// Failed TLS handshakes are expected; keep them out of the test output.
	ts.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	if tlsServer {
		ts.srv.StartTLS()
	} else {
		ts.srv.Start()
	}
	ts.addr = ts.srv.Listener.Addr().String()
	t.Cleanup(func() {
		close(stop)
		ts.srv.Close()
	})
	return ts
}

func (ts *testServer) nextConn(t *testing.T) *serverConn {
	t.Helper()
	select {
	case sc := <-ts.conns:
		return sc
	case <-time.After(waitFor):
		t.Fatal("timed out waiting for the server to accept a connection")
		return nil
	}
}

// expectMsg waits for the message named name and checks that it arrived on
// the server connection with sequence number seq.
func (ts *testServer) expectMsg(t *testing.T, name string, seq int64) {
	t.Helper()
	deadline := time.After(waitFor)
	for {
		select {
		case r := <-ts.msgs:
			if r.msg.EventName != name {
				continue
			}
			if r.sc.seq != seq {
				t.Errorf("%q arrived on server connection %d, want %d", name, r.sc.seq, seq)
			}
			return
		case <-deadline:
			t.Fatalf("timed out waiting for %q at the server", name)
		}
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitFor):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func dialTest(t *testing.T, ts *testServer, secure *Secure, h DataHandler) *Client {
	t.Helper()
	if secure == nil {
		secure = &Secure{}
	}
	if secure.Logger == nil {
		secure.Logger = discardLogger()
	}
	c, err := Dial(ts.addr, "/ws", secure, h)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func curConn(c *Client) *conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// handleEvent registers an event handler for the test and removes it
// afterwards. The events map is not locked, so register before any reader
// can look it up.
func handleEvent(t *testing.T, name string, fn EventFunc) {
	t.Helper()
	(&Client{}).HandleEvent(name, fn)
	t.Cleanup(func() { delete(events, name) })
}

// guard runs fn and turns a panic into a test error.
func guard(t *testing.T, what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	fn()
}

// TestReconnectUnderConcurrentEmit reconnects repeatedly, from several
// goroutines, while others Emit in a loop. The v1 patch panicked here with
// "send on closed channel".
func TestReconnectUnderConcurrentEmit(t *testing.T) {
	ts := newTestServer(t, false)
	h := &recHandler{}
	c := dialTest(t, ts, nil, h)

	stop := make(chan struct{})
	var emitters sync.WaitGroup
	for g := 0; g < 8; g++ {
		emitters.Add(1)
		go func() {
			defer emitters.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				guard(t, "Emit", func() { c.Emit(&common.Message{EventName: "noise"}) })
			}
		}()
	}

	const mainReconnects, extraGoroutines, extraReconnects = 25, 2, 5
	var errCount atomic.Int64
	reconnect := func() {
		guard(t, "Reconnect", func() {
			if err := c.Reconnect(); err != nil {
				errCount.Add(1)
				t.Errorf("Reconnect: %v", err)
			}
		})
	}
	var reconnectors sync.WaitGroup
	for g := 0; g < extraGoroutines; g++ {
		reconnectors.Add(1)
		go func() {
			defer reconnectors.Done()
			for i := 0; i < extraReconnects; i++ {
				reconnect()
			}
		}()
	}
	for i := 0; i < mainReconnects; i++ {
		reconnect()
	}
	reconnectors.Wait()

	close(stop)
	emittersDone := make(chan struct{})
	go func() { emitters.Wait(); close(emittersDone) }()
	waitClosed(t, emittersDone, "the Emit goroutines to return")

	if !c.IsConnected() {
		t.Error("IsConnected() = false after the reconnects")
	}
	c.Emit(&common.Message{EventName: "final"})
	ts.expectMsg(t, "final", 1+mainReconnects+extraGoroutines*extraReconnects)
	h.check(t, 1+mainReconnects+extraGoroutines*extraReconnects, 0, 0)
}

// TestReconnectNoStaleCallbacks checks that the replaced connection's reader
// fires no callbacks and does not touch the new connection when it fails.
func TestReconnectNoStaleCallbacks(t *testing.T) {
	ts := newTestServer(t, false)
	h := &recHandler{}
	c := dialTest(t, ts, nil, h)
	oldServer := ts.nextConn(t)
	old := curConn(c)

	if err := c.Reconnect(); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	ts.nextConn(t)
	_ = oldServer.closeWith(websocket.CloseInternalServerErr) // may fail: the client already closed it
	waitClosed(t, old.readerDone, "the old connection's reader to exit")

	h.check(t, 2, 0, 0)
	if !c.IsConnected() {
		t.Error("IsConnected() = false, want true")
	}
	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", 2)
	h.check(t, 2, 0, 0)
}

// TestServerDropThenReconnectFromCallback is the lb_sidecar flow:
// ConnectionClosed calls Reconnect synchronously on the reader goroutine.
func TestServerDropThenReconnectFromCallback(t *testing.T) {
	ts := newTestServer(t, false)
	var client atomic.Pointer[Client]
	reconnected := make(chan error, 1)
	h := &recHandler{onClosed: func() {
		if c := client.Load(); c != nil {
			reconnected <- c.Reconnect()
		}
	}}
	c := dialTest(t, ts, nil, h)
	client.Store(c)
	sc := ts.nextConn(t)
	old := curConn(c)

	if err := sc.closeWith(websocket.CloseInternalServerErr); err != nil {
		t.Fatalf("server close: %v", err)
	}
	select {
	case err := <-reconnected:
		if err != nil {
			t.Fatalf("Reconnect from ConnectionClosed: %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Reconnect from ConnectionClosed did not return (deadlock?)")
	}
	h.check(t, 2, 1, 1)

	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", 2)
	waitClosed(t, old.readerDone, "the old connection's reader to exit")
	h.check(t, 2, 1, 1)
	if !c.IsConnected() {
		t.Error("IsConnected() = false, want true")
	}
	client.Store(nil) // the cleanup Close must not reconnect
}

// TestConnectionClosedOnlyOnFinalClose checks that Reconnect does not fire
// ConnectionClosed, and that Close fires it once however often it is called.
func TestConnectionClosedOnlyOnFinalClose(t *testing.T) {
	ts := newTestServer(t, false)
	// Handlers registered with HandleEvent survive a reconnect.
	got := make(chan struct{}, 1)
	handleEvent(t, "server-hello", func(*common.Message) { got <- struct{}{} })

	h := &recHandler{}
	c := dialTest(t, ts, nil, h)

	for i := 0; i < 3; i++ {
		old := curConn(c)
		if err := c.Reconnect(); err != nil {
			t.Fatalf("Reconnect %d: %v", i, err)
		}
		waitClosed(t, old.readerDone, "the old connection's reader to exit")
	}
	h.check(t, 4, 0, 0)

	var sc *serverConn
	for i := 0; i < 4; i++ {
		sc = ts.nextConn(t)
	}
	if err := sc.ws.WriteJSON(&common.Message{EventName: "server-hello"}); err != nil {
		t.Fatalf("server write: %v", err)
	}
	waitClosed(t, got, "the HandleEvent handler after reconnects")

	c.Close()
	h.check(t, 4, 1, 0)
	c.Close()
	h.check(t, 4, 1, 0)
	if c.IsConnected() {
		t.Error("IsConnected() = true after Close")
	}

	// Close is final.
	if err := c.Reconnect(); !errors.Is(err, ErrClosed) {
		t.Errorf("Reconnect after Close = %v, want ErrClosed", err)
	}
	if err := c.ReconnectWithSecure(&Secure{Logger: discardLogger()}); !errors.Is(err, ErrClosed) {
		t.Errorf("ReconnectWithSecure after Close = %v, want ErrClosed", err)
	}
	h.check(t, 4, 1, 0)
}

// TestReconnectFromNewConnection checks that NewConnection can call
// Reconnect without deadlocking.
func TestReconnectFromNewConnection(t *testing.T) {
	ts := newTestServer(t, false)
	var client atomic.Pointer[Client]
	var once atomic.Bool
	nested := make(chan error, 1)
	h := &recHandler{onNewConn: func(n int64) {
		if n == 2 && once.CompareAndSwap(false, true) {
			nested <- client.Load().Reconnect()
		}
	}}
	c := dialTest(t, ts, nil, h)
	client.Store(c)

	result := make(chan error, 1)
	go func() { result <- c.Reconnect() }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Reconnect: %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Reconnect with a nested Reconnect in NewConnection did not return (deadlock?)")
	}
	if err := <-nested; err != nil {
		t.Fatalf("Reconnect from NewConnection: %v", err)
	}
	h.check(t, 3, 0, 0)
	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", 3)
}

// TestReconnectFromNewClientError checks that a reader that reported a lost
// connection skips ConnectionClosed once a Reconnect has replaced it.
func TestReconnectFromNewClientError(t *testing.T) {
	ts := newTestServer(t, false)
	var client atomic.Pointer[Client]
	reconnected := make(chan error, 1)
	h := &recHandler{onErr: func() { reconnected <- client.Load().Reconnect() }}
	c := dialTest(t, ts, nil, h)
	client.Store(c)
	sc := ts.nextConn(t)
	old := curConn(c)

	if err := sc.closeWith(websocket.CloseInternalServerErr); err != nil {
		t.Fatalf("server close: %v", err)
	}
	waitClosed(t, old.readerDone, "the old connection's reader to exit")
	if err := <-reconnected; err != nil {
		t.Fatalf("Reconnect from NewClientError: %v", err)
	}
	h.check(t, 2, 0, 1)
	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", 2)
}

// TestCloseStopsBackoffLoop runs the lb_sidecar backoff loop in
// ConnectionClosed against a stopped server; Close must end it.
func TestCloseStopsBackoffLoop(t *testing.T) {
	ts := newTestServer(t, false)
	var client atomic.Pointer[Client]
	failed := make(chan struct{}, 1)
	loopDone := make(chan error, 1)
	h := &recHandler{onClosed: func() {
		c := client.Load()
		for {
			err := c.Reconnect()
			if err == nil || errors.Is(err, ErrClosed) {
				loopDone <- err
				return
			}
			select {
			case failed <- struct{}{}:
			default:
			}
			time.Sleep(time.Millisecond)
		}
	}}
	c := dialTest(t, ts, nil, h)
	client.Store(c)
	sc := ts.nextConn(t)

	ts.srv.Close() // from now on, dials fail
	if err := sc.closeWith(websocket.CloseInternalServerErr); err != nil {
		t.Fatalf("server close: %v", err)
	}
	waitClosed(t, failed, "a failed Reconnect in the backoff loop")
	c.Close()

	select {
	case err := <-loopDone:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("backoff loop ended with %v, want ErrClosed", err)
		}
	case <-time.After(waitFor):
		t.Fatal("the backoff loop did not see ErrClosed after Close")
	}
	h.check(t, 1, 1, 1)
}

// TestEventDispatchSerialised checks that a new connection's events wait
// for an event handler still running on the replaced connection.
func TestEventDispatchSerialised(t *testing.T) {
	ts := newTestServer(t, false)
	entered := make(chan struct{})
	gate := make(chan struct{})
	second := make(chan struct{}, 1)
	handleEvent(t, "block", func(*common.Message) {
		close(entered)
		<-gate
	})
	handleEvent(t, "second", func(*common.Message) { second <- struct{}{} })

	c := dialTest(t, ts, nil, &recHandler{})
	sc1 := ts.nextConn(t)
	if err := sc1.ws.WriteJSON(&common.Message{EventName: "block"}); err != nil {
		t.Fatalf("server write: %v", err)
	}
	waitClosed(t, entered, "the blocking event handler")

	if err := c.Reconnect(); err != nil {
		close(gate)
		t.Fatalf("Reconnect: %v", err)
	}
	sc2 := ts.nextConn(t)
	if err := sc2.ws.WriteJSON(&common.Message{EventName: "second"}); err != nil {
		close(gate)
		t.Fatalf("server write: %v", err)
	}
	select {
	case <-second:
		t.Error("the new connection dispatched an event while the old connection's handler was running")
	case <-time.After(100 * time.Millisecond):
	}
	close(gate)
	waitClosed(t, second, "the new connection's event handler")
}

// TestReconnectFromEventHandler checks that an event handler can call
// Reconnect and the new connection then dispatches events.
func TestReconnectFromEventHandler(t *testing.T) {
	ts := newTestServer(t, false)
	var client atomic.Pointer[Client]
	reconnected := make(chan error, 1)
	pong := make(chan struct{}, 1)
	handleEvent(t, "reconnect", func(*common.Message) { reconnected <- client.Load().Reconnect() })
	handleEvent(t, "pong", func(*common.Message) { pong <- struct{}{} })

	h := &recHandler{}
	c := dialTest(t, ts, nil, h)
	client.Store(c)
	if err := ts.nextConn(t).ws.WriteJSON(&common.Message{EventName: "reconnect"}); err != nil {
		t.Fatalf("server write: %v", err)
	}
	select {
	case err := <-reconnected:
		if err != nil {
			t.Fatalf("Reconnect from an event handler: %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Reconnect from an event handler did not return (deadlock?)")
	}

	if err := ts.nextConn(t).ws.WriteJSON(&common.Message{EventName: "pong"}); err != nil {
		t.Fatalf("server write: %v", err)
	}
	waitClosed(t, pong, "the event handler on the new connection")
	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", 2)
	h.check(t, 2, 0, 0)
}

func certPool(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

func funcPtr(f interface{}) uintptr {
	if f == nil || reflect.ValueOf(f).IsNil() {
		return 0
	}
	return reflect.ValueOf(f).Pointer()
}

// TestReconnectWithSecureTLS checks that Reconnect reuses the stored TLS
// settings, that ReconnectWithSecure replaces them completely and keeps them
// even when the dial fails, and that websocket.DefaultDialer is left alone.
func TestReconnectWithSecureTLS(t *testing.T) {
	defaultTLS := websocket.DefaultDialer.TLSClientConfig
	defaultProxy := funcPtr(websocket.DefaultDialer.Proxy)

	ts := newTestServer(t, true)
	good := func() *Secure {
		return &Secure{EnableTLS: true, TLSConfig: &tls.Config{RootCAs: certPool(ts.srv)}, Logger: discardLogger()}
	}
	bad := &Secure{EnableTLS: true, TLSConfig: &tls.Config{RootCAs: x509.NewCertPool()}, Logger: discardLogger()}
	h := &recHandler{}
	c := dialTest(t, ts, good(), h)

	if err := c.Reconnect(); err != nil {
		t.Fatalf("Reconnect with the stored TLS settings: %v", err)
	}

	var unknownCA x509.UnknownAuthorityError
	err := c.ReconnectWithSecure(bad)
	if !errors.As(err, &unknownCA) {
		t.Fatalf("ReconnectWithSecure(untrusted CA) = %v, want x509.UnknownAuthorityError", err)
	}
	if c.IsConnected() {
		t.Error("IsConnected() = true after a failed reconnect")
	}
	if err := c.Reconnect(); !errors.As(err, &unknownCA) {
		t.Fatalf("Reconnect after ReconnectWithSecure(untrusted CA) = %v, want x509.UnknownAuthorityError", err)
	}

	if err := c.ReconnectWithSecure(good()); err != nil {
		t.Fatalf("ReconnectWithSecure(trusted CA): %v", err)
	}
	c.Emit(&common.Message{EventName: "after"})
	ts.expectMsg(t, "after", ts.seq.Load())
	h.check(t, 3, 0, 0)

	// nil is rejected rather than downgrading to plain ws.
	if err := c.ReconnectWithSecure(nil); err == nil {
		t.Fatal("ReconnectWithSecure(nil) = nil, want an error")
	}
	if !c.IsConnected() {
		t.Error("ReconnectWithSecure(nil) dropped the connection")
	}
	if err := c.Reconnect(); err != nil {
		t.Fatalf("Reconnect after ReconnectWithSecure(nil), want the stored TLS settings: %v", err)
	}
	h.check(t, 4, 0, 0)

	if websocket.DefaultDialer.TLSClientConfig != defaultTLS {
		t.Error("websocket.DefaultDialer.TLSClientConfig was changed")
	}
	if funcPtr(websocket.DefaultDialer.Proxy) != defaultProxy {
		t.Error("websocket.DefaultDialer.Proxy was changed")
	}
}

// TestSecureCopiedAtDial checks that changing the caller's Secure, or its
// TLSConfig, after Dial does not change what Reconnect uses.
func TestSecureCopiedAtDial(t *testing.T) {
	ts := newTestServer(t, true)
	secure := &Secure{EnableTLS: true, TLSConfig: &tls.Config{RootCAs: certPool(ts.srv)}, Logger: discardLogger()}
	c := dialTest(t, ts, secure, &recHandler{})

	secure.TLSConfig.RootCAs = x509.NewCertPool()
	if err := c.Reconnect(); err != nil {
		t.Fatalf("Reconnect after the caller changed its TLSConfig: %v", err)
	}

	secure.TLSConfig = &tls.Config{RootCAs: x509.NewCertPool()}
	secure.EnableTLS = false
	if err := c.Reconnect(); err != nil {
		t.Fatalf("Reconnect after the caller changed its Secure: %v", err)
	}
}

// TestCloseDuringReconnectDial calls Close while Reconnect is dialling.
// Reconnect must close the new connection and return an error.
func TestCloseDuringReconnectDial(t *testing.T) {
	ts := newTestServer(t, false)
	reached := make(chan struct{})
	gate := make(chan struct{})
	ts.beforeUpgrade = func(seq int64) {
		if seq == 2 {
			close(reached)
			<-gate
		}
	}
	h := &recHandler{}
	c := dialTest(t, ts, nil, h)
	ts.nextConn(t)

	result := make(chan error, 1)
	go func() { result <- c.Reconnect() }()
	waitClosed(t, reached, "the reconnect request at the server")
	c.Close()
	close(gate)

	select {
	case err := <-result:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Reconnect after Close during the dial = %v, want ErrClosed", err)
		}
	case <-time.After(waitFor):
		t.Fatal("Reconnect did not return")
	}
	if c.IsConnected() {
		t.Error("IsConnected() = true after Close")
	}
	h.check(t, 1, 0, 0)
	waitClosed(t, ts.nextConn(t).done, "the server to see the abandoned connection closed")
}

func TestNewForTesting(t *testing.T) {
	ch := make(chan *common.Message, 1)
	c := NewForTesting(ch)
	msg := &common.Message{EventName: "hello"}
	c.Emit(msg)
	if got := <-ch; got != msg {
		t.Errorf("emitChan got %v, want %v", got, msg)
	}

	// With a nil channel, Emit discards the message.
	c = NewForTesting(nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Emit(msg)
	}()
	waitClosed(t, done, "Emit on NewForTesting(nil) to return")

	if err := c.Reconnect(); err == nil {
		t.Error("Reconnect on a NewForTesting client returned nil")
	}
	if err := c.ReconnectWithSecure(&Secure{Logger: discardLogger()}); err == nil {
		t.Error("ReconnectWithSecure on a NewForTesting client returned nil")
	}
	if c.secure != nil || c.logger != nil {
		t.Error("ReconnectWithSecure on a NewForTesting client stored its settings")
	}
	if c.IsConnected() {
		t.Error("IsConnected() = true on a NewForTesting client")
	}
	guard(t, "Close", c.Close)
	guard(t, "Close on a zero Client", (&Client{}).Close)
}

func TestNoGoroutineLeakAcrossReconnects(t *testing.T) {
	ts := newTestServer(t, false)
	c := dialTest(t, ts, nil, &recHandler{})
	ts.nextConn(t)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		old := curConn(c)
		if err := c.Reconnect(); err != nil {
			t.Fatalf("Reconnect %d: %v", i, err)
		}
		waitClosed(t, old.readerDone, "the old connection's reader to exit")
	}
	last := curConn(c)
	c.Close()
	waitClosed(t, last.readerDone, "the last connection's reader to exit")

	deadline := time.Now().Add(2 * time.Second)
	n := runtime.NumGoroutine()
	for n > baseline+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	if n > baseline+2 {
		t.Errorf("%d goroutines after 50 reconnects and Close, baseline %d", n, baseline)
	}
}

// TestEmitAfterServerDropReportsError checks that Emit on a connection the
// server has closed reports NewClientError (as v2.2.0 did), without blocking
// or panicking.
func TestEmitAfterServerDropReportsError(t *testing.T) {
	ts := newTestServer(t, false)
	h := &recHandler{}
	c := dialTest(t, ts, nil, h)
	sc := ts.nextConn(t)
	old := curConn(c)

	// The reader treats only 1001 and 1006 as expected closes.
	if err := sc.closeWith(websocket.CloseGoingAway); err != nil {
		t.Fatalf("server close: %v", err)
	}
	waitClosed(t, old.readerDone, "the reader to exit")
	h.check(t, 1, 1, 0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		guard(t, "Emit", func() { c.Emit(&common.Message{EventName: "late"}) })
	}()
	waitClosed(t, done, "Emit on a dropped connection to return")
	h.check(t, 1, 1, 1)
}
