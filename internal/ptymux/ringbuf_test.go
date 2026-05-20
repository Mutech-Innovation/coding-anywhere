package ptymux

import (
	"bytes"
	"testing"
)

func TestRingBufferWriteFastPath(t *testing.T) {
	r := newRingBuffer(16)
	r.Write([]byte("hello"))
	if got := r.Snapshot(); !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("snapshot: %q", got)
	}
	if size, written, overflow := r.Stats(); size != 5 || written != 5 || overflow != 0 {
		t.Fatalf("stats: size=%d written=%d overflow=%d", size, written, overflow)
	}
}

func TestRingBufferEvictsOldest(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("12345"))
	r.Write([]byte("67890"))
	if got := r.Snapshot(); !bytes.Equal(got, []byte("34567890")) {
		t.Fatalf("snapshot: %q", got)
	}
	_, _, overflow := r.Stats()
	if overflow != 2 {
		t.Fatalf("overflow: %d", overflow)
	}
}

func TestRingBufferAcceptsLargerThanCapacity(t *testing.T) {
	r := newRingBuffer(4)
	r.Write([]byte("0123456789"))
	if got := r.Snapshot(); !bytes.Equal(got, []byte("6789")) {
		t.Fatalf("snapshot: %q", got)
	}
	_, written, overflow := r.Stats()
	if written != 10 || overflow != 6 {
		t.Fatalf("written=%d overflow=%d", written, overflow)
	}
}

func TestRingBufferReset(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("hello"))
	r.Reset()
	if size, _, _ := r.Stats(); size != 0 {
		t.Fatalf("size after reset: %d", size)
	}
}
