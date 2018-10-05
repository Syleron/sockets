package sockets

var events map[string]EventFunc

type EventFunc func(msg Message)

func EventHandler(msg Message) {
	event := events[msg.EventName]
	if event != nil {
		event(msg)
	}
}
