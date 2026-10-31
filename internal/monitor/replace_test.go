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
	m.AddPrinter(cfg)
	p := m.find("p")
	p.geom = Geometry{State: GeometryReady, Key: "k", Uploaded: true}
	p.job = "a\x001"
	return m, p, cfg
}

func TestRenameKeepsThePrinter(t *testing.T) {
	m, p, cfg := added(t)
	cfg.Name = "New"
	if err := m.ReplacePrinter(cfg); err != nil {
		t.Fatal(err)
	}
	if m.find("p") != p {
		t.Fatal("renaming reconnected the printer")
	}
	if info := p.info(); info.Name != "New" || info.Geometry.Key != "k" {
		t.Fatalf("name %q, geometry %+v", info.Name, info.Geometry)
	}
	cfg.Name = ""
	if err := m.ReplacePrinter(cfg); err != nil || p.info().Name != "p" {
		t.Fatalf("name %q without a configured one, err %v", p.info().Name, err)
	}
}

func TestReconnectKeepsGeometryAndCamera(t *testing.T) {
	m, old, cfg := added(t)
	cfg.Host = "10.0.0.6"
	if err := m.ReplacePrinter(cfg); err != nil {
		t.Fatal(err)
	}
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
	if err := m.ReplacePrinter(cfg); err != nil {
		t.Fatal(err)
	}
	if g := m.find("p").info().Geometry; g.State != GeometryNone {
		t.Errorf("geometry %+v kept for another printer", g)
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
