package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatesBeforeTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bblv.log")
	l, err := Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for range 7 {
		if _, err = l.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	// 7 lines of 40 bytes: 2 in each full file, the last one in the new file.
	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".1")
	if len(cur) != 40 || len(old) != 80 {
		t.Fatalf("bblv.log has %d bytes, bblv.log.1 %d; want 40 and 80", len(cur), len(old))
	}

	// Reopening continues the file and counts what it holds.
	if l, err = Open(path, 100); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Write([]byte(line))
	l.Write([]byte(line))
	if cur, _ = os.ReadFile(path); len(cur) != 40 {
		t.Fatalf("bblv.log has %d bytes after reopening, want a new file with one line", len(cur))
	}
}
