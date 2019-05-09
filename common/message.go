package common

type Message struct {
	EventName string      `json:"eventName"`
	Data      interface{} `json:"data"`
}
