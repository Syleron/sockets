package sockets

type Response struct {
	EventName string `json:"eventName"`
	Data interface{} `json:"data"`
}