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

import "time"

type Config struct {
	// Time allowed to write a message to the peer.
	WriteWait time.Duration
	// Time allowed to read the next pong message from the peer.
	PongWait time.Duration
	// Send pings to peer with this period. Must be less than pongWait.
	PingPeriod time.Duration
	// Maximum message size allowed from peer.
	ReadLimitSize int64
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
