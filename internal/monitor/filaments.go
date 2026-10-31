package monitor

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/marektews/bblv/internal/atomicfile"
	"github.com/marektews/bblv/internal/gcode"
	"github.com/marektews/bblv/internal/printer"
)

// filamentNames maps the IDs of filament profiles, all the AMS reports of a
// spool's profile, to their names. Print files name the profiles they use,
// so the names are learned from them and kept in a file across restarts.
type filamentNames struct {
	mu    sync.Mutex
	path  string // "" keeps the names only in memory
	names map[string]string
}

func (f *filamentNames) load(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &f.names)
}

// learn remembers the filament profiles of a print and tells whether any
// name was new. A project may hold several profiles with the same ID, e.g.
// copies named "…-1"; the first one is taken.
func (f *filamentNames) learn(s *gcode.Settings) bool {
	if s == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := false
	seen := map[string]bool{}
	for i, id := range s.FilamentIDs {
		if i >= len(s.FilamentProfiles) || id == "" || s.FilamentProfiles[i] == "" || seen[id] {
			continue
		}
		seen[id] = true
		if f.names[id] != s.FilamentProfiles[i] {
			if f.names == nil {
				f.names = map[string]string{}
			}
			f.names[id] = s.FilamentProfiles[i]
			changed = true
		}
	}
	if changed && f.path != "" {
		if err := f.saveLocked(); err != nil {
			slog.Warn("saving filament names failed", "file", f.path, "err", err)
		}
	}
	return changed
}

func (f *filamentNames) saveLocked() error {
	b, err := json.MarshalIndent(f.names, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(f.path, b, 0o600)
}

// nameTrays returns copies of the AMS units and the external spool with the
// names of the spools' profiles filled in where known.
func (f *filamentNames) nameTrays(units []printer.AMS, ext *printer.Tray) ([]printer.AMS, *printer.Tray) {
	f.mu.Lock()
	defer f.mu.Unlock()
	named := make([]printer.AMS, len(units))
	for i, u := range units {
		u.Trays = slices.Clone(u.Trays)
		for j := range u.Trays {
			u.Trays[j].Profile = f.names[u.Trays[j].ProfileID]
		}
		named[i] = u
	}
	if ext != nil {
		t := *ext
		t.Profile = f.names[t.ProfileID]
		ext = &t
	}
	return named, ext
}
