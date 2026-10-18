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
		if _, err := c.Put(key, ".bin", func(w io.Writer) error {
			_, err := io.WriteString(w, strings.Repeat("x", 10))
			return err
		}); err != nil {
			t.Fatal(err)
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
