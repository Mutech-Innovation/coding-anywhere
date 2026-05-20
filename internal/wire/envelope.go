package wire

import (
	"encoding/json"
	"errors"
)

// EnvelopeType disambiguates the three message flavours on the wire.
type EnvelopeType string

const (
	TypeRequest  EnvelopeType = "req"
	TypeResponse EnvelopeType = "res"
	TypeEvent    EnvelopeType = "event"
)

// Channel namespaces the envelope payload. The set must stay in sync with
// packages/protocol/src/envelope.ts.
type Channel string

const (
	ChannelMeta          Channel = "meta"
	ChannelSessions      Channel = "sessions"
	ChannelChat          Channel = "chat"
	ChannelTerminal      Channel = "terminal"
	ChannelFiles         Channel = "files"
	ChannelDiff          Channel = "diff"
	ChannelGit           Channel = "git"
	ChannelPorts         Channel = "ports"
	ChannelNotifications Channel = "notifications"
)

// Envelope is the WebSocket-frame-level shape every message uses.
//
// `Payload` is kept as `json.RawMessage` so the relay can route without
// decoding the application-level body. Each channel decodes the payload
// into its own typed struct on the client/daemon side.
type Envelope struct {
	ID      string          `json:"id"`
	Type    EnvelopeType    `json:"type"`
	Channel Channel         `json:"channel"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *EnvelopeError  `json:"error,omitempty"`
}

// EnvelopeError is set on `res` envelopes when the request failed.
type EnvelopeError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// MakeRequest builds a `req` envelope.
func MakeRequest(id string, channel Channel, method string, payload any) (Envelope, error) {
	return makeEnvelope(id, TypeRequest, channel, method, payload, nil)
}

// MakeResponse builds a `res` envelope correlated with `id`.
func MakeResponse(id string, channel Channel, payload any, errBody *EnvelopeError) (Envelope, error) {
	return makeEnvelope(id, TypeResponse, channel, "", payload, errBody)
}

// MakeEvent builds an `event` envelope. `id` is allocated by the sender and
// is not correlated to any prior request.
func MakeEvent(id string, channel Channel, method string, payload any) (Envelope, error) {
	return makeEnvelope(id, TypeEvent, channel, method, payload, nil)
}

func makeEnvelope(id string, t EnvelopeType, channel Channel, method string, payload any, errBody *EnvelopeError) (Envelope, error) {
	if id == "" {
		return Envelope{}, errors.New("envelope id required")
	}
	env := Envelope{ID: id, Type: t, Channel: channel, Method: method, Error: errBody}
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		env.Payload = buf
	}
	return env, nil
}

// ValidChannel reports whether `c` is one of the known channel names.
func ValidChannel(c Channel) bool {
	switch c {
	case ChannelMeta, ChannelSessions, ChannelChat, ChannelTerminal,
		ChannelFiles, ChannelDiff, ChannelGit, ChannelPorts, ChannelNotifications:
		return true
	}
	return false
}
