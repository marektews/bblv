package monitor

import (
	"testing"

	"github.com/marektews/bblv/internal/config"
)

// added returns a monitor, not running, with one network printer showing
// the geometry "k".
func added(t *testing.T) (*Monitor, *Printer, config.Printer) {
	t.Helper()
	cfg := config.Printer{ID: "p", Name: "Old", Model: "A1", Host: "10.0.0.5", Serial: "S1", AccessCode: "12345678"}
	m := New(nil, "")
	m.SyncPrinters([]config.Printer{cfg})
	p := m.find("p")
	p.geom = Geometry{State: GeometryReady, Key: "k", Uploaded: true}
	p.job = "a\x001"
	return m, p, cfg
}

func TestRenameKeepsThePrinter(t *testing.T) {
	m, p, cfg := added(t)
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	cfg.Name = "New"
	m.SyncPrinters([]config.Printer{cfg})
	if m.find("p") != p {
		t.Fatal("renaming reconnected the printer")
	}
	if info := p.info(); info.Name != "New" || info.Geometry.Key != "k" {
		t.Fatalf("name %q, geometry %+v", info.Name, info.Geometry)
	}
	m.SyncPrinters([]config.Printer{cfg}) // e.g. after another printer changed
	if n := len(events); n != 1 {
		t.Fatalf("%d events, want only the one of the new name", n)
	}
	cfg.Name = ""
	m.SyncPrinters([]config.Printer{cfg})
	if name := p.info().Name; name != "p" {
		t.Fatalf("name %q without a configured one", name)
	}
}

func TestReconnectKeepsGeometryAndCamera(t *testing.T) {
	m, old, cfg := added(t)
	cfg.Host = "10.0.0.6"
	m.SyncPrinters([]config.Printer{cfg})
	p := m.find("p")
	if p == old || !old.removed.Load() {
		t.Fatal("the printer was not reconnected")
	}
	if p.camera != old.camera {
		t.Error("the camera hub was replaced, which ends the streams of its viewers")
	}
	if g := p.info().Geometry; g.Key != "k" || !g.Uploaded || p.job != old.job {
		t.Errorf("geometry %+v, job %q", g, p.job)
	}

	// Another serial number is another printer.
	cfg.Serial = "S2"
	m.SyncPrinters([]config.Printer{cfg})
	if g := m.find("p").info().Geometry; g.State != GeometryNone {
		t.Errorf("geometry %+v kept for another printer", g)
	}
}

func TestSyncAddsAndRemovesButKeepsTheReplay(t *testing.T) {
	m, old, cfg := added(t)
	m.add(newPrinter(m, "replay", "Replay", "A1", nil))
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	other := config.Printer{ID: "q", Model: "P1S", Host: "10.0.0.7", Serial: "S3"}
	m.SyncPrinters([]config.Printer{other})

	var ids []string
	for _, info := range m.Printers() {
		ids = append(ids, info.ID)
	}
	if len(ids) != 2 || ids[0] != "replay" || ids[1] != "q" {
		t.Fatalf("printers %v, want the replay and q", ids)
	}
	if !old.removed.Load() || cameraOpen(old) {
		t.Fatal("the printer no longer configured still runs")
	}
	removed, printer := <-events, <-events
	if removed.Type != "removed" || removed.Info.ID != cfg.ID || printer.Type != "printer" || printer.Info.ID != "q" {
		t.Fatalf("events %s %s, %s %s", removed.Type, removed.Info.ID, printer.Type, printer.Info.ID)
	}
}

// cameraOpen reports whether the camera of a printer still accepts
// viewers; a removed printer's does not.
func cameraOpen(p *Printer) bool {
	frames, unsubscribe := p.camera.Subscribe()
	defer unsubscribe()
	select {
	case _, open := <-frames:
		return open
	default:
		return true
	}
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	m := New(nil, "")
	events, unsubscribe := m.Subscribe()
	defer unsubscribe()
	for range subscriberBuffer + 1 {
		m.publish(Event{Type: "printer"})
	}
	n := 0
	for range events {
		n++
	}
	if n != subscriberBuffer {
		t.Fatalf("received %d events before the channel closed, want %d", n, subscriberBuffer)
	}
}

func TestGeometryInUse(t *testing.T) {
	m, _, _ := added(t)
	if keys := m.geometryKeys(); len(keys) != 1 || keys[0] != "k" {
		t.Fatalf("keys = %v", keys)
	}
}
