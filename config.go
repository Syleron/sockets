// MIT License
//
// Copyright (c) 2022 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
/// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
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

package sockets

import (
	"log/slog"
	"time"
)

type Config struct {
	// Time allowed to write a message to the peer: each Emit and each ping
	// must complete within it, or the write fails and the connection is
	// closed. Zero means the default (10s); a negative value disables the
	// deadline, so a write to a peer that stops reading can block forever.
	WriteWait time.Duration
	// Time allowed to read the next pong message from the peer.
	PongWait time.Duration
	// Send pings to peer with this period. Must be less than pongWait.
	PingPeriod time.Duration
	// Maximum message size allowed from peer.
	ReadLimitSize int64
	// Logger receives the library's log records. Optional: when nil, the
	// library logs to slog.Default(), resolved at log time, so a later
	// slog.SetDefault is honoured. Failures (upgrade, emit, close, invalid
	// input) are logged at Warn; connection, room and session lifecycle
	// events at Debug; shutdown at Info. To silence the library, pass a
	// logger whose handler discards records.
	Logger *slog.Logger
}

// logger returns the configured logger, or slog.Default() if none is set.
func (c *Config) logger() *slog.Logger {
	if c != nil && c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// MergeDefaults sets the uninitialized fields in the config with default values.
func (c *Config) MergeDefaults() {
	defaults := DefaultConfig()
	if c.WriteWait == 0 {
		c.WriteWait = defaults.WriteWait
	}
	if c.PongWait == 0 {
		c.PongWait = defaults.PongWait
	}
	if c.PingPeriod == 0 {
		c.PingPeriod = defaults.PingPeriod
	}
	if c.ReadLimitSize == 0 {
		c.ReadLimitSize = defaults.ReadLimitSize
	}
}

// DefaultConfig returns a configuration with default settings.
func DefaultConfig() Config {
	c := Config{
		WriteWait:     10 * time.Second,
		PongWait:      60 * time.Second,
		ReadLimitSize: 2560,
	}
	c.PingPeriod = (c.PongWait * 9) / 10
	return c
}
