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
	"fmt"

	"github.com/syleron/sockets/common"
)

var events map[string]*Event

type Event struct {
	Protected bool
	EventFunc EventFunc
}

type EventFunc func(msg *common.Message, ctx *Context)

func EventHandler(msg *common.Message, ctx *Context) {
	event := events[msg.EventName]
	if event != nil {
		// Check to see if we are protected
		if event.Protected {
			if ctx.HasSession() {
				event.EventFunc(msg, ctx)
			} else {
				fmt.Print("protected " + msg.EventName + " event called, however, no session has been set. Handler dropped. " + ctx.UUID)
			}
		} else {
			event.EventFunc(msg, ctx)
		}
	} else {
		fmt.Print("event " + msg.EventName + " does not have an event handler")
	}
}
