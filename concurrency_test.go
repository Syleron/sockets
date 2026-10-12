package sockets

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/syleron/sockets/v2/common"
)

// ctxHandler hands each new server-side Context to the test.
type ctxHandler struct {
	opened chan *Context
	closed chan struct{} // optional
	// onOpen, if set, runs on the connection's own goroutine before the read
	// loop starts (as lb_central does when it registers a session).
	onOpen func(*Context)
}

func (h *ctxHandler) NewConnection(ctx *Context) {
	if h.onOpen != nil {
		h.onOpen(ctx)
	}
	h.opened <- ctx
}
func (h *ctxHandler) ConnectionClosed(*Context) {
	if h.closed != nil {
		h.closed <- struct{}{}
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// frameReader reads frames on a client connection and counts them. Every
// frame must be a text frame holding well-formed JSON.
type frameReader struct {
	good, bad atomic.Int64
	done      chan struct{}
	firstBad  atomic.Value // string
}

func readFrames(conn *websocket.Conn) *frameReader {
	r := &frameReader{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage || !json.Valid(data) {
				if r.bad.Add(1) == 1 {
					r.firstBad.Store(fmt.Sprintf("type=%d len=%d %.80q", mt, len(data), data))
				}
				continue
			}
			r.good.Add(1)
		}
	}()
	return r
}

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func waitCtx(t *testing.T, ch <-chan *Context) *Context {
	t.Helper()
	select {
	case ctx := <-ch:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server NewConnection")
		return nil
	}
}

// guard runs fn and turns a panic into a test error, so a concurrent write
// panic in gorilla/websocket is reported rather than crashing the binary.
func guard(t *testing.T, what string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", what, r)
		}
	}()
	fn()
}

// TestConcurrentWrites_OneConnection drives every server write path at once
// against one connection: Context.Emit, Broadcast, BroadcastToRoom,
// BroadcastToRoomChannel, Session.Emit and the keepalive ping (1ms period).
// gorilla/websocket allows one concurrent writer per connection, so without
// per-connection serialisation this panics ("concurrent write to websocket
// connection"), trips the race detector, or delivers corrupt frames.
func TestConcurrentWrites_OneConnection(t *testing.T) {
	h := &ctxHandler{opened: make(chan *Context, 1)}
	s := New(h, &Config{PingPeriod: time.Millisecond, Logger: discardLogger()})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.HandleConnection(w, r, "")
	}))
	defer srv.Close()

	conn := dial(t, srv)
	defer func() { _ = conn.Close() }()
	frames := readFrames(conn)

	ctx := waitCtx(t, h.opened)
	if err := s.AddSession("alice", ctx.Connection); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := s.JoinRoom("room", ctx.UUID); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if err := s.JoinRoomChannel("chan", ctx.UUID); err != nil {
		t.Fatalf("JoinRoomChannel: %v", err)
	}
	// BroadcastToRoom* skip the sender, so send as another connection.
	other := &Context{UUID: "other"}
	session := ctx.Session

	// A payload large enough that frames take a while to write, so
	// unsynchronised writers overlap.
	payload := strings.Repeat("x", 4096)
	rawPayload := json.RawMessage(`"` + payload + `"`)
	writers := []struct {
		name string
		fn   func()
	}{
		{"Context.Emit", func() { _ = ctx.Emit(&common.Message{EventName: "emit", Data: rawPayload}) }},
		{"Broadcast", func() { s.Broadcast("all", payload) }},
		{"BroadcastToRoom", func() { s.BroadcastToRoom("room", "room", payload, other) }},
		{"BroadcastToRoomChannel", func() { s.BroadcastToRoomChannel("room", "chan", "chan", payload, other) }},
		{"Session.Emit", func() { session.Emit(&common.Message{EventName: "session", Data: rawPayload}) }},
	}
	const perWriter = 4   // goroutines per write path
	const iterations = 50 // writes per goroutine

	var wg sync.WaitGroup
	for _, w := range writers {
		for g := 0; g < perWriter; g++ {
			wg.Add(1)
			go func(name string, fn func()) {
				defer wg.Done()
				for i := 0; i < iterations; i++ {
					guard(t, name, fn)
				}
			}(w.name, w.fn)
		}
	}
	wg.Wait()

	want := int64(len(writers) * perWriter * iterations)
	deadline := time.Now().Add(10 * time.Second)
	for frames.good.Load()+frames.bad.Load() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if bad := frames.bad.Load(); bad != 0 {
		t.Errorf("client read %d malformed frames, first: %v", bad, frames.firstBad.Load())
	}
	if good := frames.good.Load(); good != want {
		t.Errorf("client read %d well-formed JSON frames, want %d", good, want)
	}
}

// TestSessionEmit_ConcurrentTabs opens and closes several connections for
// one user (browser tabs) while Session.Emit runs in a loop. Session.Emit
// must not read the session's connection map while connections are added
// (UpdateSession) or removed (disconnect), and every write must be
// serialised per connection.
func TestSessionEmit_ConcurrentTabs(t *testing.T) {
	var s *Sockets
	h := &ctxHandler{opened: make(chan *Context, 16)}
	h.onOpen = func(ctx *Context) {
		// lb_central's pattern: create the session, or join the existing one.
		if err := s.AddSession("bob", ctx.Connection); err != nil {
			if err := s.UpdateSession("bob", ctx.Connection); err != nil {
				t.Errorf("UpdateSession: %v", err)
			}
		}
	}
	s = New(h, &Config{PingPeriod: time.Millisecond, Logger: discardLogger()})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.HandleConnection(w, r, "")
	}))
	defer srv.Close()

	first := dial(t, srv)
	defer func() { _ = first.Close() }()
	firstFrames := readFrames(first)
	session := waitCtx(t, h.opened).Session

	stop := make(chan struct{})
	var emitters sync.WaitGroup
	for g := 0; g < 4; g++ {
		emitters.Add(1)
		go func() {
			defer emitters.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				guard(t, "Session.Emit", func() {
					session.Emit(&common.Message{EventName: "session", Data: json.RawMessage(`"tab"`)})
				})
			}
		}()
	}

	const tabs = 8
	var tabsWG sync.WaitGroup
	for i := 0; i < tabs; i++ {
		tabsWG.Add(1)
		go func() {
			defer tabsWG.Done()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
			if err != nil {
				t.Errorf("dial tab: %v", err)
				return
			}
			r := readFrames(conn)
			time.Sleep(20 * time.Millisecond)
			_ = conn.Close()
			<-r.done
			if bad := r.bad.Load(); bad != 0 {
				t.Errorf("tab read %d malformed frames, first: %v", bad, r.firstBad.Load())
			}
		}()
	}
	tabsWG.Wait()
	for i := 0; i < tabs; i++ {
		waitCtx(t, h.opened)
	}
	close(stop)
	emitters.Wait()

	if bad := firstFrames.bad.Load(); bad != 0 {
		t.Errorf("first tab read %d malformed frames, first: %v", bad, firstFrames.firstBad.Load())
	}
	if firstFrames.good.Load() == 0 {
		t.Error("first tab received no frames from Session.Emit")
	}
}

// slowPeer starts a server with cfg and connects a client that never reads,
// so the server's send buffer fills and writes to it block.
func slowPeer(t *testing.T, cfg *Config) (*Sockets, *ctxHandler, *Context) {
	t.Helper()
	h := &ctxHandler{opened: make(chan *Context, 1), closed: make(chan struct{}, 1)}
	cfg.Logger = discardLogger()
	s := New(h, cfg)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.HandleConnection(w, r, "")
	}))
	t.Cleanup(srv.Close)

	conn := dial(t, srv) // never read from: the peer is stuck
	t.Cleanup(func() { _ = conn.Close() })
	return s, h, waitCtx(t, h.opened)
}

// blockBroadcast broadcasts a large payload until one Broadcast takes at
// least writeWait/2 (it blocked on the stuck peer), while repeatedly taking
// the registry write lock via JoinRoom. It returns how long the blocked
// Broadcast took and the longest JoinRoom wait. It fails the test if no
// Broadcast returns within 15s.
func blockBroadcast(t *testing.T, s *Sockets, writeWait time.Duration) (blocked, maxJoin time.Duration) {
	t.Helper()
	payload := strings.Repeat("x", 256<<10)
	var d atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		end := time.Now().Add(10 * time.Second)
		for time.Now().Before(end) {
			start := time.Now()
			s.Broadcast("big", payload)
			if took := time.Since(start); took >= writeWait/2 {
				d.Store(int64(took))
				return
			}
		}
	}()

	// The UUID does not exist; only the wait for the registry lock matters.
	giveUp := time.After(15 * time.Second)
	for {
		select {
		case <-done:
			blocked = time.Duration(d.Load())
			if blocked == 0 {
				t.Fatal("Broadcast never blocked on the stuck peer; the send buffer did not fill")
			}
			return blocked, maxJoin
		case <-giveUp:
			t.Fatal("Broadcast to a stuck peer did not return within 15s")
		case <-time.After(10 * time.Millisecond):
			start := time.Now()
			_ = s.JoinRoom("room", "no-such-uuid")
			if took := time.Since(start); took > maxJoin {
				maxJoin = took
			}
		}
	}
}

// TestSlowPeer_EmitWriteDeadline checks the Emit write deadline on its own.
// The keepalive is an hour apart, so nothing but Config.WriteWait can unblock
// a write to a peer that never reads. A Broadcast that blocks on that peer
// must give up after about WriteWait without holding the registry lock
// (JoinRoom stays fast), and a later Emit to the peer must fail rather than
// block.
func TestSlowPeer_EmitWriteDeadline(t *testing.T) {
	const writeWait = time.Second
	s, _, ctx := slowPeer(t, &Config{WriteWait: writeWait, PingPeriod: time.Hour})

	blocked, maxJoin := blockBroadcast(t, s, writeWait)
	t.Logf("blocked Broadcast returned after %v; max JoinRoom wait %v", blocked, maxJoin)
	if blocked > writeWait+2*time.Second {
		t.Errorf("blocked Broadcast took %v, want about WriteWait (%v)", blocked, writeWait)
	}
	if maxJoin > 500*time.Millisecond {
		t.Errorf("JoinRoom waited %v for the registry lock while Broadcast was blocked on a stuck peer", maxJoin)
	}

	start := time.Now()
	err := ctx.Emit(&common.Message{EventName: "after-timeout"})
	took := time.Since(start)
	t.Logf("Emit to the stuck peer returned %v after %v", err, took)
	if err == nil {
		t.Error("Emit to a peer whose write timed out returned nil, want an error")
	}
	if took > writeWait+2*time.Second {
		t.Errorf("Emit to the stuck peer took %v, want at most about WriteWait (%v)", took, writeWait)
	}
}

// TestSlowPeer_KeepaliveClosesConnection checks that once a write to a stuck
// peer has timed out, the keepalive (short PingPeriod here) closes the
// connection so the read loop cleans it up.
func TestSlowPeer_KeepaliveClosesConnection(t *testing.T) {
	const writeWait = time.Second
	s, h, _ := slowPeer(t, &Config{WriteWait: writeWait, PingPeriod: 50 * time.Millisecond})

	blocked, maxJoin := blockBroadcast(t, s, writeWait)
	t.Logf("blocked Broadcast returned after %v; max JoinRoom wait %v", blocked, maxJoin)
	if maxJoin > 500*time.Millisecond {
		t.Errorf("JoinRoom waited %v for the registry lock while Broadcast was blocked on a stuck peer", maxJoin)
	}
	select {
	case <-h.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not close the connection after the write timeout")
	}
}

// TestEmit_LeavesNoStaleWriteDeadline checks that Emit clears the write
// deadline it sets, so a later write on the same Conn (after WriteWait has
// passed) is not failed by a deadline left over from Emit.
func TestEmit_LeavesNoStaleWriteDeadline(t *testing.T) {
	const writeWait = 50 * time.Millisecond
	h := &ctxHandler{opened: make(chan *Context, 1)}
	s := New(h, &Config{WriteWait: writeWait, PingPeriod: time.Hour, Logger: discardLogger()})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.HandleConnection(w, r, "")
	}))
	defer srv.Close()

	conn := dial(t, srv)
	defer func() { _ = conn.Close() }()
	frames := readFrames(conn)
	ctx := waitCtx(t, h.opened)

	if err := ctx.Emit(&common.Message{EventName: "first"}); err != nil {
		t.Fatalf("first Emit: %v", err)
	}
	time.Sleep(3 * writeWait)
	// Direct write on the test goroutine (the only writer; pings are an hour
	// apart). With a stale deadline this fails with an i/o timeout.
	if err := ctx.Conn.WriteMessage(websocket.TextMessage, []byte(`{"eventName":"direct"}`)); err != nil {
		t.Fatalf("write after Emit's deadline had passed: %v", err)
	}
	if err := ctx.Emit(&common.Message{EventName: "second"}); err != nil {
		t.Fatalf("second Emit: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for frames.good.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := frames.good.Load(); got != 3 || frames.bad.Load() != 0 {
		t.Fatalf("client read %d good and %d bad frames, want 3 and 0", got, frames.bad.Load())
	}
}

// TestCheckIfSessionExists_ConcurrentWithSessionChanges reads the session
// registry while sessions are added and deleted. Without the registry read
// lock this is a data race (and can be a fatal concurrent map read and write).
func TestCheckIfSessionExists_ConcurrentWithSessionChanges(t *testing.T) {
	s := New(&ctxHandler{opened: make(chan *Context, 1)}, &Config{Logger: discardLogger()})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.CheckIfSessionExists("carol")
			}
		}
	}()
	for i := 0; i < 500; i++ {
		_ = s.AddSession("carol", NewConnection())
		_ = s.DeleteSession("carol")
	}
	close(stop)
	wg.Wait()
}
