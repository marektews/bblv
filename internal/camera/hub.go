package camera

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Hub shares one camera stream between all viewers. The source runs while
// anyone watches and for a grace period after the last viewer leaves; it is
// restarted after a delay when it fails.
type Hub struct {
	name   string
	source Source
	grace  time.Duration
	retry  time.Duration

	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	latest []byte
	cancel context.CancelFunc // set while the source runs
	idle   *time.Timer
	closed bool
}

// NewHub creates a hub; name identifies the camera in logs.
func NewHub(name string, source Source) *Hub {
	return &Hub{name: name, source: source, grace: 10 * time.Second, retry: 5 * time.Second, subs: map[chan []byte]struct{}{}}
}

// Subscribe returns a channel that always holds the newest frame not yet
// received, and a function to unsubscribe.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[ch] = struct{}{}
	if h.latest != nil {
		ch <- h.latest
	}
	if h.idle != nil {
		h.idle.Stop()
		h.idle = nil
	}
	if h.cancel == nil && !h.closed {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	return ch, func() { h.unsubscribe(ch) }
}

// Close stops the stream for good, e.g. when the printer is removed.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.latest = nil
	if h.idle != nil {
		h.idle.Stop()
		h.idle = nil
	}
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

func (h *Hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
	if len(h.subs) == 0 && h.cancel != nil && h.idle == nil {
		h.idle = time.AfterFunc(h.grace, h.stopIfIdle)
	}
}

func (h *Hub) stopIfIdle() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.idle = nil
	if len(h.subs) == 0 && h.cancel != nil {
		h.cancel()
		h.cancel = nil
		h.latest = nil
	}
}

func (h *Hub) run(ctx context.Context) {
	for {
		err := h.source(ctx, h.publish)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("camera stream stopped, reconnecting", "camera", h.name, "in", h.retry, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(h.retry):
		}
	}
}

func (h *Hub) publish(img []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = img
	for ch := range h.subs {
		// Replace a frame the viewer has not taken yet; only this function sends.
		select {
		case <-ch:
		default:
		}
		ch <- img
	}
}
