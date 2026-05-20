package ptymux

import "sync"

// ringBuffer is a fixed-capacity byte buffer with FIFO eviction. It is safe
// for concurrent use by a single writer and many readers (Snapshot).
//
// Used as a per-session scrollback so that clients reconnecting to a
// terminal can replay the most recent N bytes of output before going live.
type ringBuffer struct {
	mu       sync.RWMutex
	data     []byte
	cap      int
	written  int64 // total bytes ever written (monotonic)
	overflow int64 // bytes evicted because of capacity
}

// newRingBuffer returns an empty buffer with the given capacity.
func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 64 << 10
	}
	return &ringBuffer{cap: capacity, data: make([]byte, 0, capacity)}
}

// Write appends b to the buffer. Bytes that don't fit in `cap` push the
// oldest content out. Returns the number of bytes appended.
func (r *ringBuffer) Write(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.written += int64(len(b))
	// Fast path: still room.
	if len(r.data)+len(b) <= r.cap {
		r.data = append(r.data, b...)
		return len(b)
	}
	// Slow path: trim oldest then append. If the input itself is bigger than
	// capacity, keep only its tail.
	if len(b) >= r.cap {
		dropped := len(r.data) + (len(b) - r.cap)
		r.overflow += int64(dropped)
		r.data = append(r.data[:0], b[len(b)-r.cap:]...)
		return len(b)
	}
	want := r.cap - len(b)
	dropped := 0
	if len(r.data) > want {
		dropped = len(r.data) - want
		copy(r.data, r.data[dropped:])
		r.data = r.data[:want]
	}
	r.overflow += int64(dropped)
	r.data = append(r.data, b...)
	return len(b)
}

// Snapshot returns a copy of the current contents.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]byte, len(r.data))
	copy(out, r.data)
	return out
}

// Stats reports the lifetime byte counts.
func (r *ringBuffer) Stats() (size, written, overflow int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.data)), r.written, r.overflow
}

// Reset truncates the buffer to empty without changing its capacity. Used
// when a session is reused (currently we don't, but keep the surface tidy).
func (r *ringBuffer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = r.data[:0]
	r.written = 0
	r.overflow = 0
}
