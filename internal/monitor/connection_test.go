package monitor

import (
	"errors"
	"testing"
)

func TestConnectionErrorOnlyWhileOffline(t *testing.T) {
	m := New(nil, "")
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	p := newPrinter(m, "p", "", "A1", nil)
	m.printers = append(m.printers, p)

	p.onConnectError(errors.New("the certificate is not signed by the CA learned for this printer"))
	p.onConnectError(errors.New("the certificate is not signed by the CA learned for this printer"))
	if n := len(events); n != 1 {
		t.Fatalf("%d events, want one for the new reason", n)
	}
	if got := p.info().ConnectionError; got == "" {
		t.Fatal("reason not shown for an offline printer")
	}
	p.onConnectError(nil)
	p.onConnection(true)
	if got := p.info().ConnectionError; got != "" {
		t.Fatalf("reason %q shown for a connected printer", got)
	}
}
