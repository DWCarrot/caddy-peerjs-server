package protocol

import (
	"encoding/json"
	"fmt"
)

type MessageType string

const (
	OPEN      MessageType = "OPEN"
	LEAVE     MessageType = "LEAVE"
	CANDIDATE MessageType = "CANDIDATE"
	OFFER     MessageType = "OFFER"
	ANSWER    MessageType = "ANSWER"
	EXPIRE    MessageType = "EXPIRE"
	HEARTBEAT MessageType = "HEARTBEAT"
	IDTAKEN   MessageType = "ID-TAKEN"
	ERROR     MessageType = "ERROR"
)

// IMessage struct that maps to the interface in TypeScript
type Message struct {
	Type    MessageType     `json:"type"`
	Src     string          `json:"src,omitempty"`
	Dst     string          `json:"dst,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Payload struct {
	Msg string `json:"msg"`
}

func (p Payload) IntoJsonRaw() json.RawMessage {
	b, _ := json.Marshal(p)
	return json.RawMessage(b)
}

// Build Id-Taken error message
func BuildIdTaken(id string, taken string) *Message {
	return &Message{
		Type:    IDTAKEN,
		Payload: Payload{Msg: "ID is taken"}.IntoJsonRaw(),
	}
}

// Build Error message
func BuildError(msg string) *Message {
	return &Message{
		Type:    ERROR,
		Payload: Payload{Msg: msg}.IntoJsonRaw(),
	}
}

// Build Open message
func BuildOpen(id string) *Message {
	return &Message{
		Type: OPEN,
	}
}

type UnknownMessageError struct {
	Type string
}

func (e UnknownMessageError) Error() string {
	return fmt.Sprintf("UnknownMessageError{ Type: %s }", e.Type)
}

var (
	_ error = (*UnknownMessageError)(nil)
)
