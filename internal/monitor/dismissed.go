package monitor

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/marektews/bblv/internal/atomicfile"
	"github.com/marektews/bblv/internal/hms"
	"github.com/marektews/bblv/internal/printer"
)

// alertKeys names the alerts of a status: "error:0700_8011" for the print
// error, "hms:0300_0100_0001_0007" for every HMS alert.
func alertKeys(s printer.Status) []string {
	var keys []string
	if s.PrintError != 0 {
		keys = append(keys, hms.KindError+":"+hms.ErrorCode(s.PrintError))
	}
	for _, h := range s.HMS {
		keys = append(keys, hms.KindHMS+":"+h.String())
	}
	return keys
}

// dismissedAlerts are the alerts someone has read and removed: they are
// hidden, for everyone, until the printer stops reporting them, so that
// one coming back shows again. They are kept in a file across restarts.
type dismissedAlerts struct {
	mu        sync.Mutex
	path      string              // "" keeps them only in memory
	byPrinter map[string][]string // alert keys by printer ID
}

func (d *dismissedAlerts) load(path string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &d.byPrinter)
}

// list returns the dismissed alerts of a printer, never nil.
func (d *dismissedAlerts) list(id string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string{}, d.byPrinter[id]...)
}

// change applies f to the keys of a printer and saves them when they changed.
func (d *dismissedAlerts) change(id string, f func([]string) []string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.byPrinter[id]
	keys := f(slices.Clone(old))
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if slices.Equal(keys, old) {
		return false
	}
	if d.byPrinter == nil {
		d.byPrinter = map[string][]string{}
	}
	if len(keys) == 0 {
		delete(d.byPrinter, id)
	} else {
		d.byPrinter[id] = keys
	}
	if d.path != "" {
		if b, err := json.MarshalIndent(d.byPrinter, "", "  "); err == nil {
			if err = atomicfile.Write(d.path, b, 0o600); err != nil {
				slog.Warn("dismissed alerts not saved", "file", d.path, "err", err)
			}
		}
	}
	return true
}

// dismiss hides those of keys the printer reports now.
func (d *dismissedAlerts) dismiss(id string, keys, current []string) bool {
	return d.change(id, func(old []string) []string {
		for _, k := range keys {
			if slices.Contains(current, k) {
				old = append(old, k)
			}
		}
		return old
	})
}

// restore shows keys again.
func (d *dismissedAlerts) restore(id string, keys []string) bool {
	return d.change(id, func(old []string) []string {
		return slices.DeleteFunc(old, func(k string) bool { return slices.Contains(keys, k) })
	})
}

// keepPrinters forgets the alerts of printers other than ids, e.g. removed
// while the server was not running.
func (d *dismissedAlerts) keepPrinters(ids []string) {
	d.mu.Lock()
	var gone []string
	for id := range d.byPrinter {
		if !slices.Contains(ids, id) {
			gone = append(gone, id)
		}
	}
	d.mu.Unlock()
	for _, id := range gone {
		d.retain(id, nil)
	}
}

// retain forgets the alerts the printer no longer reports.
func (d *dismissedAlerts) retain(id string, current []string) bool {
	return d.change(id, func(old []string) []string {
		return slices.DeleteFunc(old, func(k string) bool { return !slices.Contains(current, k) })
	})
}
