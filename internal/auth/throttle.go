package auth

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// maxWait is the longest wait between login attempts after failures.
const maxWait = 30 * time.Second

// throttle holds password checks back after failures, for every client at
// once: delaying each request only would let parallel ones guess faster,
// each costing a bcrypt comparison.
type throttle struct {
	mu       sync.Mutex
	checking bool      // one password is checked at a time
	failures int       // since the last success
	until    time.Time // no check before
}

// WaitError is returned for a login attempt while failed ones hold further
// attempts back.
type WaitError struct {
	Wait time.Duration
}

func (e *WaitError) Error() string {
	return fmt.Sprintf("too many wrong passwords, try again in %d s", e.Seconds())
}

// Seconds returns the wait rounded up to whole seconds, at least 1.
func (e *WaitError) Seconds() int {
	return max(1, int(math.Ceil(e.Wait.Seconds())))
}

// checkWait is how long a password check waits for another one in
// progress, e.g. of a second login at the same time or a double click.
const checkWait = 2 * time.Second

// begin starts a password check, or returns how long to wait first.
func (t *throttle) begin(now time.Time) *WaitError {
	t.mu.Lock()
	defer t.mu.Unlock()
	for waited := time.Duration(0); t.checking && waited < checkWait; waited += 20 * time.Millisecond {
		t.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		t.mu.Lock()
	}
	switch {
	case t.checking:
		return &WaitError{Wait: time.Second}
	case now.Before(t.until):
		return &WaitError{Wait: t.until.Sub(now)}
	}
	t.checking = true
	return nil
}

// end finishes a check; every failure in a row doubles the wait before the
// next one, from 1 s up to maxWait.
func (t *throttle) end(ok bool, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checking = false
	if ok {
		t.failures, t.until = 0, time.Time{}
		return
	}
	t.failures++
	t.until = now.Add(min(time.Second<<min(t.failures-1, 10), maxWait))
}
