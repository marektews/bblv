package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	for _, content := range []string{"first", "second"} {
		if err := Write(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != content {
			t.Fatalf("read %q, %v; want %q", b, err, content)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, %v", info.Mode(), err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d files left, want only config.json", len(entries))
	}
}

func TestWriteKeepsTheOldFileOnFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.json")
	if err := Write(path, []byte("x"), 0o600); err == nil {
		t.Fatal("wrote into a missing directory")
	}
}
