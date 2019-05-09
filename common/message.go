package common

import (
	"encoding/json"
)

type Response struct {
	EventName string `json:"eventName"`
	Data interface{} `json:"data"`
}

type Message struct {
	EventName string          `json:"eventName"`
	Data      json.RawMessage `json:"data"`
}

