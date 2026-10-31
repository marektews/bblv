// Package atomicfile replaces files so that a crash or a power cut leaves
// either the old or the new content, never an empty or partial file.
package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
)

// Write replaces the file at path with data: it writes a temporary file in
// the same directory, flushes it to the disk, gives it perm and renames it
// over path, then flushes the directory entry where the system allows it.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // fails harmlessly after the rename
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Without this, the rename may reach the disk before the data, which
	// after a power cut leaves an empty file.
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir makes the rename durable. Windows cannot open a directory for it
// and keeps renames on NTFS in its journal.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
