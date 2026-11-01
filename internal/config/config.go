// Package config loads and saves the server configuration kept in the data directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/marektews/bblv/internal/atomicfile"
)

// ErrNotFound is returned for an unknown printer id.
var ErrNotFound = errors.New("printer not found")

// FileName is the name of the configuration file inside the data directory.
const FileName = "config.json"

// Printer describes one printer reachable in the local network.
type Printer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	Host       string `json:"host"`
	Serial     string `json:"serial"`
	AccessCode string `json:"access_code"`
}

// Config is the whole content of config.json.
type Config struct {
	PasswordHash  string `json:"password_hash,omitempty"`
	SessionSecret string `json:"session_secret,omitempty"` // signs login cookies; replaced with the password
	// RevokedSessions lists the sessions logged out before they expired:
	// a hash of the cookie and its expiry unix time.
	RevokedSessions map[string]int64 `json:"revoked_sessions,omitempty"`
	Printers        []Printer        `json:"printers"`
	Ntfy            Ntfy             `json:"ntfy,omitzero"`
}

// Ntfy is where notifications go besides the browsers: an ntfy topic, which
// the ntfy app shows on a phone also while no page of bblv is open.
type Ntfy struct {
	URL    string   `json:"url,omitempty"`    // of the server, e.g. https://ntfy.sh
	Topic  string   `json:"topic,omitempty"`  // "" turns ntfy off
	Token  string   `json:"token,omitempty"`  // access token of a protected topic
	Events []string `json:"events,omitempty"` // kinds of notify events to send
	Lang   string   `json:"lang,omitempty"`   // of the messages: pl or en
	Link   string   `json:"link,omitempty"`   // address of bblv a notification opens, "" for none
}

// DefaultDataDir returns the OS specific default data directory.
func DefaultDataDir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("ProgramData"); d != "" {
			return filepath.Join(d, "bblv")
		}
		return `C:\ProgramData\bblv`
	}
	return "/var/lib/bblv"
}

// Load reads config.json from dataDir. A missing file yields an empty config.
func Load(dataDir string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if err = c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return &c, nil
}

// validate rejects printers without an id or sharing one; the id selects the
// printer everywhere, so such printers would hide each other.
func (c *Config) validate() error {
	seen := map[string]bool{}
	for i, p := range c.Printers {
		if p.ID == "" {
			return fmt.Errorf("printer %d (host %q) has no id", i+1, p.Host)
		}
		if seen[p.ID] {
			return fmt.Errorf("printer id %q is used more than once", p.ID)
		}
		seen[p.ID] = true
	}
	return nil
}

// Save writes config.json atomically and durably with owner-only permissions.
func Save(dataDir string, c *Config) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dataDir, FileName), b, 0o600)
}

// Printer returns the printer with the given id. An empty id selects the only
// configured printer.
func (c *Config) Printer(id string) (*Printer, error) {
	if id == "" {
		if len(c.Printers) == 1 {
			return &c.Printers[0], nil
		}
		return nil, fmt.Errorf("%d printers configured, choose one by id", len(c.Printers))
	}
	for i := range c.Printers {
		if c.Printers[i].ID == id {
			return &c.Printers[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
}
