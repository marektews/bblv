package cache

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, 25)
	if err != nil {
		t.Fatal(err)
	}
	put := func(key string) {
		if _, putErr := c.Put(key, ".bin", func(w io.Writer) error {
			_, werr := io.WriteString(w, strings.Repeat("x", 10))
			return werr
		}); putErr != nil {
			t.Fatal(putErr)
		}
	}
	put("a")
	put("b")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "a.bin"), old, old)
	os.Chtimes(filepath.Join(dir, "b.bin"), old.Add(time.Minute), old.Add(time.Minute))
	if _, ok := c.Get("a", ".bin"); !ok { // a becomes the most recently used
		t.Fatal("a missing")
	}
	put("c") // 30 bytes > 25: b is the least recently used
	if _, ok := c.Get("b", ".bin"); ok {
		t.Error("b should have been evicted")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.Get(k, ".bin"); !ok {
			t.Errorf("%s evicted", k)
		}
	}
}

func TestEvictsWholeEntriesNotInUse(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir, 35)
	if err != nil {
		t.Fatal(err)
	}
	put := func(key, ext string) {
		if _, putErr := c.Put(key, ext, func(w io.Writer) error {
			_, werr := io.WriteString(w, strings.Repeat("x", 10))
			return werr
		}); putErr != nil {
			t.Fatal(putErr)
		}
	}
	age := func(key, ext string, d time.Duration) {
		at := time.Now().Add(-d)
		os.Chtimes(filepath.Join(dir, key+ext), at, at)
	}
	c.KeepInUse(func() []string { return []string{"shown"} })
	put("shown", ".bin")
	age("shown", ".bin", 3*time.Hour) // the oldest, but a printer shows it
	put("old", ".bin")
	put("old", ".json")
	age("old", ".bin", 2*time.Hour)
	age("old", ".json", 2*time.Hour)
	put("new", ".bin") // 40 bytes > 35
	if _, ok := c.Get("shown", ".bin"); !ok {
		t.Error("geometry in use evicted")
	}
	for _, ext := range []string{".bin", ".json"} {
		if _, ok := c.Get("old", ext); ok {
			t.Errorf("old%s kept, want the whole entry evicted", ext)
		}
	}
	if _, ok := c.Get("new", ".bin"); !ok {
		t.Error("new evicted")
	}
}
