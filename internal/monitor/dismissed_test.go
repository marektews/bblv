package monitor

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marektews/bblv/internal/printer"
)

// A dismissed alert stays hidden for everyone, also after a restart, until
// the printer stops reporting it; coming back, it shows again.
func TestDismissedAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dismissed-alerts.json")
	m := New(nil, "")
	if err := m.KeepDismissedAlerts(path); err != nil {
		t.Fatal(err)
	}
	p := newPrinter(m, "a1", "", "A1", nil)
	m.printers = append(m.printers, p)
	ctx := context.Background()
	alerting := printer.Status{State: "PAUSE", PrintError: 0x07008011, HMS: []printer.HMS{{Attr: 0x07008000, Code: 0x00020011}}}
	p.onStatus(ctx, alerting)

	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	if err := m.DismissAlerts("a1", []string{"error:0700_8011", "hms:0000_0000_0000_0000"}); err != nil {
		t.Fatal(err)
	}
	if got := p.info().DismissedAlerts; !slices.Equal(got, []string{"error:0700_8011"}) {
		t.Fatalf("dismissed %v, want only the alert reported", got)
	}
	if len(events) != 1 {
		t.Fatal("dismissing not told to the pages")
	}

	// After a restart, the same.
	restarted := New(nil, "")
	if err := restarted.KeepDismissedAlerts(path); err != nil {
		t.Fatal(err)
	}
	if got := restarted.dismissed.list("a1"); !slices.Equal(got, []string{"error:0700_8011"}) {
		t.Fatalf("after restart %v", got)
	}

	// Gone and back: shown again.
	cleared := alerting
	cleared.PrintError = 0
	p.onStatus(ctx, cleared)
	p.onStatus(ctx, alerting)
	if got := p.info().DismissedAlerts; len(got) != 0 {
		t.Fatalf("alert that came back still hidden: %v", got)
	}

	if err := m.DismissAlerts("a1", alertKeys(alerting)); err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreAlerts("a1", []string{"hms:0700_8000_0002_0011"}); err != nil {
		t.Fatal(err)
	}
	if got := p.info().DismissedAlerts; !slices.Equal(got, []string{"error:0700_8011"}) {
		t.Fatalf("after restoring one: %v", got)
	}
	if err := m.DismissAlerts("nope", nil); err == nil {
		t.Fatal("unknown printer accepted")
	}
}
