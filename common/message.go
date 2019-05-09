package common

import (
	"encoding/json"
	"bytes"
	"encoding/gob"
)

type Message struct {
	EventName string      `json:"eventName"`
	Data      interface{} `json:"data"`
}

func (m *Message) DataAsRaw() json.RawMessage {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(m.Data)
	if err != nil {
		return nil
	}
	return buf.Bytes()
}
