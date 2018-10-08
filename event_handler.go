package sockets

var events map[string]EventFunc

type EventFunc func(msg *Message, ctx *Context)

func EventHandler(msg *Message, ctx *Context) {
	event := events[msg.EventName]
	if event != nil {
		event(msg, ctx)
	}
}
