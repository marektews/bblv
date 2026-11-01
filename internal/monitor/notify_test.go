package monitor

import (
	"context"
	"slices"
	"testing"

	"github.com/marektews/bblv/internal/notify"
	"github.com/marektews/bblv/internal/printer"
)

// notices drains the events of a subscription and returns the notifications.
func notices(events <-chan Event) []notify.Event {
	var out []notify.Event
	for {
		select {
		case e := <-events:
			if e.Type == "notification" {
				out = append(out, *e.Notice)
			}
		default:
			return out
		}
	}
}

// A notification follows a change between two reports, also when the
// printer was offline in between, but not the first report.
func TestNotificationsOnChanges(t *testing.T) {
	m := New(nil, "")
	var sent []notify.Event
	m.OnNotice(func(e notify.Event) { sent = append(sent, e) })
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	p := newPrinter(m, "a1", "Biuro", "A1", nil)
	m.printers = append(m.printers, p)
	ctx := context.Background()

	running := printer.Status{State: "RUNNING", Subtask: "Most", HMS: []printer.HMS{}}
	p.onStatus(ctx, running)
	if got := notices(events); len(got) != 0 {
		t.Fatalf("first report told %+v", got)
	}

	p.onConnection(false) // the printer drops off and finishes meanwhile
	finished := running
	finished.State = "FINISH"
	p.onStatus(ctx, finished)
	got := notices(events)
	if len(got) != 1 || !slices.Equal(got[0].Kinds, []string{notify.Finish}) || got[0].Printer != "a1" || got[0].Name != "Biuro" ||
		got[0].Subtask != "Most" || got[0].ID == 0 {
		t.Fatalf("notifications %+v", got)
	}
	if len(sent) != 1 || sent[0].ID != got[0].ID {
		t.Fatalf("sent %+v, want the same notification", sent)
	}

	// A printer no longer monitored tells nothing.
	p.removed.Store(true)
	p.onStatus(ctx, running)
	p.onStatus(ctx, finished)
	if got = notices(events); len(got) != 0 || len(sent) != 1 {
		t.Fatalf("removed printer told %+v", got)
	}
}
