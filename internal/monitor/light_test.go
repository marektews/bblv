package monitor

import (
	"context"
	"errors"
	"testing"

	"github.com/marektews/bblv/internal/printer"
)

// switchSource records the light commands of a printer.
type switchSource struct{ commands []bool }

func (s *switchSource) run(context.Context, func(printer.Status), func(bool)) {}

func (s *switchSource) setLight(on bool) error {
	s.commands = append(s.commands, on)
	return nil
}

func TestSetLight(t *testing.T) {
	m := New(nil, "")
	src := &switchSource{}
	p := newPrinter(m, "a1", "", "A1", src)
	m.printers = append(m.printers, p)

	if err := m.SetLight("a1", true); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("before the printer reported a light: %v", err)
	}
	p.status.ChamberLight = "off"
	if err := m.SetLight("a1", true); err != nil || len(src.commands) != 1 || !src.commands[0] {
		t.Fatalf("switching on: %v, commands %v", err, src.commands)
	}
	if err := m.SetLight("gone", true); !errors.Is(err, ErrUnknownPrinter) {
		t.Fatalf("unknown printer: %v", err)
	}

	replay := newPrinter(m, "replay", "", "A1", &replaySource{})
	replay.status.ChamberLight = "off"
	m.printers = append(m.printers, replay)
	if err := m.SetLight("replay", true); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("replay: %v", err)
	}
}
