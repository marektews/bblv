package config

import (
	"errors"
	"sync"
	"testing"
)

// Concurrent changes reach the watcher in the order they were saved, each
// with the configuration it saved.
func TestWatchSeesEveryChangeInOrder(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var seen []int // number of printers in each configuration seen
	s.Watch(func(c Config) { seen = append(seen, len(c.Printers)) })

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Update(func(c *Config) error {
				c.Printers = append(c.Printers, Printer{ID: c.NewID("p"), Serial: string(rune('A' + i))})
				return nil
			})
		}()
	}
	wg.Wait()
	if err = s.Update(func(*Config) error { return errors.New("refused") }); err == nil {
		t.Fatal("failed change saved")
	}
	if len(seen) != 21 {
		t.Fatalf("watched %d configurations, want the first and 20 changes", len(seen))
	}
	for i, n := range seen {
		if n != i {
			t.Fatalf("configurations seen with %v printers, want 0 to 20 in order", seen)
		}
	}
}
