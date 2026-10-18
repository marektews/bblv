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

// Status returns a typed snapshot of the state.
func (s *State) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statusFrom(s.m)
}
