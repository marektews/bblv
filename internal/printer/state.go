package printer

import (
	"encoding/json"
	"sync"
)

// State is the printer state merged from consecutive reports, which may be
// full (after pushall) or contain only the changed fields.
type State struct {
	mu sync.Mutex
	m  map[string]any
}

// NewState returns an empty state.
func NewState() *State {
	return &State{m: map[string]any{}}
}

// Apply merges one raw report into the state.
func (s *State) Apply(payload []byte) error {
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		return err
	}
	s.mu.Lock()
	merge(s.m, msg)
	s.mu.Unlock()
	return nil
}

// Complete tells whether the state holds what the printer reports in full:
// its state and its alerts. After connecting, the answer to get_version
// comes first, and the A1 and P1 may send reports of only the changed
// fields before the full one; a status taken before would tell no state
// and no alerts.
func (s *State) Complete() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := obj(s.m, "print")
	_, state := p["gcode_state"]
	_, hms := p["hms"]
	return state && hms
}

// Status returns a typed snapshot of the state.
func (s *State) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statusFrom(s.m)
}
