// Package logfile writes a log file that keeps its size within a limit: when
// it would grow beyond it, it becomes <name>.1, replacing the one before,
// and a new file starts.
package logfile

import (
	"os"
	"sync"
)

// File is an append-only log file safe for concurrent writes.
type File struct {
	path  string
	limit int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens or creates the file at path, owner-only, to append to it.
func Open(path string, limit int64) (*File, error) {
	l := &File{path: path, limit: limit}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *File) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.f, l.size = f, info.Size()
	return nil
}

// Write appends p, first starting a new file when p would not fit. A
// record is never split between two files.
func (l *File) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.limit {
		// On failure the old file is kept and grows; logging goes on.
		if err := l.rotate(); err != nil && l.f == nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *File) rotate() error {
	// Windows cannot rename an open file.
	if err := l.f.Close(); err != nil {
		return err
	}
	l.f = nil
	renameErr := os.Rename(l.path, l.path+".1")
	if err := l.open(); err != nil {
		return err
	}
	return renameErr
}

// Close closes the file.
func (l *File) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
