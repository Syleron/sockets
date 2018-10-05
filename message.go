package sockets

import "encoding/json"

type Message struct {
	EventName string          `json:"eventName"`
	Username  string          `json:"username"`
	Data      json.RawMessage `json:"data"`
}

