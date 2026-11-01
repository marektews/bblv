package config

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Store keeps the configuration in memory and saves every change to disk.
type Store struct {
	dir   string
	mu    sync.Mutex
	c     Config
	watch func(Config)
}

// Watch calls f with the configuration now and after every saved change, in
// the order of the changes, so that what f keeps up to date never applies an
// older change after a newer one. f runs with the store locked: it must be
// quick and must not use the store.
func (s *Store) Watch(f func(Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watch = f
	f(s.c.clone())
}

// OpenStore loads config.json from dataDir.
func OpenStore(dataDir string) (*Store, error) {
	c, err := Load(dataDir)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dataDir, c: *c}, nil
}

// Get returns a copy of the configuration.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.clone()
}

// Update applies fn to a copy of the configuration and saves it; nothing
// changes when fn or saving fails.
func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.c.clone()
	if err := fn(&c); err != nil {
		return err
	}
	if err := Save(s.dir, &c); err != nil {
		return err
	}
	s.c = c
	if s.watch != nil {
		s.watch(c.clone())
	}
	return nil
}

func (c Config) clone() Config {
	c.Printers = slices.Clone(c.Printers)
	c.RevokedSessions = maps.Clone(c.RevokedSessions)
	c.Ntfy.Events = slices.Clone(c.Ntfy.Events)
	return c
}

// NewID derives a URL friendly printer id from its name that is not used yet.
func (c *Config) NewID(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if t, ok := transliteration[r]; ok {
			r = t
		}
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.TrimSuffix(b.String(), "-")
	if base == "" {
		base = "printer"
	}
	id := base
	for n := 2; ; n++ {
		if _, err := c.Printer(id); err != nil {
			return id
		}
		id = base + "-" + strconv.Itoa(n)
	}
}

var transliteration = map[rune]rune{'ą': 'a', 'ć': 'c', 'ę': 'e', 'ł': 'l', 'ń': 'n', 'ó': 'o', 'ś': 's', 'ź': 'z', 'ż': 'z'}
