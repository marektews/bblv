// Package recording stores raw printer reports in JSON Lines files.
package recording

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one line of a recording. The first line has Kind "meta", the rest "report".
type Entry struct {
	Time    time.Time       `json:"t"`
	Kind    string          `json:"kind"`
	Model   string          `json:"model,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Recorder appends entries to a recording file.
type Recorder struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// Create creates the file at path and writes the meta entry. The MQTT topic
// (which contains the printer serial) is not stored, but payloads are kept
// verbatim and may still include serial numbers, e.g. in get_version replies.
func Create(path, model string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	r := &Recorder{f: f, enc: enc}
	if err := enc.Encode(Entry{Time: time.Now(), Kind: "meta", Model: model}); err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// Write appends a report. Payloads that are not valid JSON are kept as a JSON string.
func (r *Recorder) Write(t time.Time, payload []byte) error {
	raw := json.RawMessage(payload)
	if !json.Valid(payload) {
		b, err := json.Marshal(string(payload))
		if err != nil {
			return err
		}
		raw = b
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enc.Encode(Entry{Time: t, Kind: "report", Payload: raw})
}

// Close closes the file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
