package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreUpdateSavesAndKeepsStateOnError(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(func(c *Config) error {
		c.Printers = append(c.Printers, Printer{ID: "a1", Host: "10.0.0.2"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.Update(func(c *Config) error {
		c.Printers[0].Host = "changed"
		return errors.New("rejected")
	})
	if got := s.Get().Printers[0].Host; got != "10.0.0.2" {
		t.Fatalf("host = %q after a failed update", got)
	}
	loaded, err := Load(dir)
	if err != nil || len(loaded.Printers) != 1 || loaded.Printers[0].ID != "a1" {
		t.Fatalf("saved config = %+v, %v", loaded, err)
	}
}

func TestNewID(t *testing.T) {
	c := &Config{Printers: []Printer{{ID: "bambus-a1"}}}
	for name, want := range map[string]string{
		"Bambus A1":      "bambus-a1-2",
		"Żółta drukarka": "zolta-drukarka",
		"  X1C (biuro) ": "x1c-biuro",
		"***":            "printer",
	} {
		if got := c.NewID(name); got != want {
			t.Errorf("NewID(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLoadRejectsMissingAndDuplicateIDs(t *testing.T) {
	for name, printers := range map[string]string{
		"duplicate": `[{"id": "a1", "host": "10.0.0.2"}, {"id": "a1", "host": "10.0.0.3"}]`,
		"missing":   `[{"id": "a1", "host": "10.0.0.2"}, {"host": "10.0.0.3"}]`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"printers": `+printers+`}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if c, err := Load(dir); err == nil {
			t.Errorf("%s: loaded %+v, want an error", name, c.Printers)
		}
	}
}
