package sockets

import (
	"bytes"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/syleron/sockets/v2/client"
	"github.com/syleron/sockets/v2/common"
)

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func debugLogger(w *syncBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestConfigLogger_DefaultsToSlogDefault(t *testing.T) {
	var nilCfg *Config
	if got := nilCfg.logger(); got != slog.Default() {
		t.Fatal("nil *Config should log to slog.Default()")
	}
	if got := (&Config{}).logger(); got != slog.Default() {
		t.Fatal("Config without Logger should log to slog.Default()")
	}
	l := slog.New(slog.NewTextHandler(&syncBuffer{}, nil))
	if got := (&Config{Logger: l}).logger(); got != l {
		t.Fatal("Config.Logger should be used when set")
	}
	// MergeDefaults must not overwrite a caller's logger.
	c := &Config{Logger: l}
	c.MergeDefaults()
	if c.Logger != l {
		t.Fatal("MergeDefaults replaced Config.Logger")
	}
}

type serverHandler struct {
	opened chan struct{}
	closed chan struct{}
}

func (h *serverHandler) NewConnection(*Context)    { h.opened <- struct{}{} }
func (h *serverHandler) ConnectionClosed(*Context) { h.closed <- struct{}{} }

type clientHandler struct {
	errs chan error
}

func (h *clientHandler) NewConnection()           {}
func (h *clientHandler) ConnectionClosed()        {}
func (h *clientHandler) NewClientError(err error) { h.errs <- err }

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestRoundTrip_LogsGoToConfigLogger runs a real server and client over
// gorilla/websocket, and checks that every library log record goes to the
// configured loggers and nothing goes to the stdlib log package.
func TestRoundTrip_LogsGoToConfigLogger(t *testing.T) {
	var stdlib syncBuffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&stdlib)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	var serverLogs, clientLogs syncBuffer
	sh := &serverHandler{opened: make(chan struct{}, 1), closed: make(chan struct{}, 1)}
	s := New(sh, &Config{Logger: debugLogger(&serverLogs)})

	got := make(chan struct{}, 1)
	s.HandleEvent("ping", func(msg *common.Message, ctx *Context) {
		if err := ctx.Emit(&common.Message{EventName: "pong"}); err != nil {
			t.Errorf("emit: %v", err)
		}
	}, false)
	s.HandleEvent("secret", func(*common.Message, *Context) {
		t.Error("protected handler ran without a session")
	}, true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// An invalid realIP must be logged at Warn and fall back to RemoteAddr.
		_ = s.HandleConnection(w, r, "not-an-ip\nforged=1")
	}))
	defer srv.Close()

	ch := &clientHandler{errs: make(chan error, 4)}
	c, err := client.Dial(strings.TrimPrefix(srv.URL, "http://"), "/ws",
		&client.Secure{Logger: debugLogger(&clientLogs)}, ch)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.HandleEvent("pong", func(*common.Message) { got <- struct{}{} })
	waitFor(t, sh.opened, "server NewConnection")

	c.Emit(&common.Message{EventName: "secret"})
	c.Emit(&common.Message{EventName: "unknown"})
	c.Emit(&common.Message{EventName: "ping"})
	waitFor(t, got, "pong")

	c.Close()
	waitFor(t, sh.closed, "server ConnectionClosed")

	logs := serverLogs.String()
	for _, want := range []string{
		`level=DEBUG msg="connection added"`,
		`level=WARN msg="invalid realIP provided, using remote address" realIP="not-an-ip\nforged=1"`,
		`level=WARN msg="protected event called without a session, handler dropped" event=secret`,
		`level=DEBUG msg="no handler registered for event" event=unknown`,
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("server logs missing %q\n%s", want, logs)
		}
	}
	// Attacker-controlled values are quoted, so they cannot forge records.
	if strings.Contains(logs, "\nforged=1") {
		t.Errorf("unescaped newline from realIP reached the log:\n%s", logs)
	}
	if out := stdlib.String(); out != "" {
		t.Errorf("library wrote to the stdlib log package:\n%s", out)
	}
}

func TestEventHandler_UsesSlogDefault(t *testing.T) {
	var buf syncBuffer
	prev := slog.Default()
	slog.SetDefault(debugLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(prev) })

	EventHandler(&common.Message{EventName: "no-such-event"}, &Context{Connection: NewConnection()})
	if !strings.Contains(buf.String(), `msg="no handler registered for event" event=no-such-event`) {
		t.Fatalf("EventHandler did not log to slog.Default(): %q", buf.String())
	}
}
