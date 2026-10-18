package config

import (
	"errors"
	"testing"
)

func TestStoreUpdateSavesAndKeepsStateOnError(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) error {
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
