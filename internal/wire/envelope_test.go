package wire_test

import (
	"encoding/json"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

func TestMakeRequestRoundTrip(t *testing.T) {
	env, err := wire.MakeRequest("abc", wire.ChannelMeta, "ping", map[string]string{"nonce": "n1"})
	if err != nil {
		t.Fatalf("MakeRequest: %v", err)
	}
	if env.Type != wire.TypeRequest {
		t.Fatalf("got type %q", env.Type)
	}
	buf, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back wire.Envelope
	if err := json.Unmarshal(buf, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.ID != "abc" || back.Channel != wire.ChannelMeta || back.Method != "ping" {
		t.Fatalf("unexpected: %+v", back)
	}
}

func TestMakeResponseError(t *testing.T) {
	env, err := wire.MakeResponse("abc", wire.ChannelMeta, nil, &wire.EnvelopeError{
		Code:    "not_found",
		Message: "no such session",
	})
	if err != nil {
		t.Fatalf("MakeResponse: %v", err)
	}
	if env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("error not set: %+v", env)
	}
}

func TestRejectsEmptyID(t *testing.T) {
	if _, err := wire.MakeRequest("", wire.ChannelMeta, "x", nil); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestValidChannel(t *testing.T) {
	cases := []struct {
		c    wire.Channel
		want bool
	}{
		{wire.ChannelMeta, true},
		{wire.ChannelTerminal, true},
		{wire.Channel("mystery"), false},
	}
	for _, tc := range cases {
		if got := wire.ValidChannel(tc.c); got != tc.want {
			t.Errorf("ValidChannel(%q) = %v, want %v", tc.c, got, tc.want)
		}
	}
}
