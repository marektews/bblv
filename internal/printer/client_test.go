package printer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"

	"github.com/marektews/bblv/internal/trust"
)

func TestConnectHint(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w : %w", packets.ErrorRefusedNotAuthorised, errors.New("x")), "access code"},
		{packets.ErrorRefusedBadUsernameOrPassword, "access code"},
		{fmt.Errorf("%w : %w", packets.ErrorNetworkError, errors.New("dial tcp: i/o timeout")), "port 8883"},
	} {
		if got := connectHint(tc.err); !strings.Contains(got, tc.want) {
			t.Errorf("connectHint(%v) = %q, want it to mention %q", tc.err, got, tc.want)
		}
	}
	// Certificate errors say what to do; checking the address would mislead.
	for _, err := range []error{trust.ErrUntrusted, &trust.FileError{Name: "printer-ca.pem", Err: errors.New("x")}} {
		if got := connectHint(fmt.Errorf("%w : %w", packets.ErrorNetworkError, err)); got != "" {
			t.Errorf("connectHint(%v) = %q, want none", err, got)
		}
	}
}

type fakeToken struct {
	done bool
	err  error
}

func (t fakeToken) Wait() bool                     { return t.done }
func (t fakeToken) WaitTimeout(time.Duration) bool { return t.done }
func (t fakeToken) Done() <-chan struct{}          { return nil }
func (t fakeToken) Error() error                   { return t.err }

// fakeBroker answers subscriptions with the given tokens in turn; the
// other methods of mqtt.Client are not used.
type fakeBroker struct {
	mqtt.Client
	open    bool
	answers []fakeToken
	calls   int
}

func (b *fakeBroker) IsConnectionOpen() bool { return b.open }

func (b *fakeBroker) Subscribe(string, byte, mqtt.MessageHandler) mqtt.Token {
	t := b.answers[min(b.calls, len(b.answers)-1)]
	b.calls++
	return t
}

// A subscription the printer does not confirm in time is tried again; the
// printer is reported online only once one succeeds.
func TestSubscribeRetries(t *testing.T) {
	failed := errors.New("refused")
	for _, tc := range []struct {
		name   string
		broker *fakeBroker
		ok     bool
		calls  int
	}{
		{"at once", &fakeBroker{open: true, answers: []fakeToken{{done: true}}}, true, 1},
		{"after a timeout", &fakeBroker{open: true, answers: []fakeToken{{}, {done: true}}}, true, 2},
		{"never", &fakeBroker{open: true, answers: []fakeToken{{done: true, err: failed}}}, false, 3},
		{"disconnected", &fakeBroker{answers: []fakeToken{{done: true}}}, false, 0},
	} {
		err := subscribe(tc.broker, "device/X/report", nil)
		if (err == nil) != tc.ok || tc.broker.calls != tc.calls {
			t.Errorf("%s: err = %v after %d attempts, want ok=%v after %d", tc.name, err, tc.broker.calls, tc.ok, tc.calls)
		}
	}
}
