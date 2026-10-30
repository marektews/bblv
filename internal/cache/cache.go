// Package cache keeps derived files (geometry, thumbnails) on disk within a size limit.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultLimit is the default total size of the cache.
const DefaultLimit = 1 << 30

// Cache stores files named <key><ext> in one directory and evicts the least
// recently used ones when the total size exceeds the limit.
type Cache struct {
	dir   string
	limit int64
	mu    sync.Mutex
}

// Open creates the directory if needed and removes leftovers of interrupted writes.
func Open(dir string, limit int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	for _, f := range tmp {
		os.Remove(f)
	}
	return &Cache{dir: dir, limit: limit}, nil
}

// Key derives a cache key from the given parts.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Get returns the path of a cached file and marks it as recently used.
func (c *Cache) Get(key, ext string) (string, bool) {
	p := filepath.Join(c.dir, key+ext)
	now := time.Now()
	if err := os.Chtimes(p, now, now); err != nil {
		return "", false
	}
	return p, true
}

// Put stores what write produces under key and evicts old files over the limit.
func (c *Cache) Put(key, ext string, write func(io.Writer) error) (string, error) {
	tmp, err := os.CreateTemp(c.dir, key+"-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err = write(tmp); err != nil {
		tmp.Close()
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	p := filepath.Join(c.dir, key+ext)
	if err = os.Rename(tmp.Name(), p); err != nil {
		return "", err
	}
	c.evict(p)
	return p, nil
}

func (c *Cache) evict(keep string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, file{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= c.limit {
			break
		}
		if f.path != keep && os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
