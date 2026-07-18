package main

import "sync"

// rowcountState keeps per-target baselines for the lifetime of one daemon.
type rowcountState struct {
	mu     sync.Mutex
	values map[string]int64
}

func newRowcountState() *rowcountState {
	return &rowcountState{values: make(map[string]int64)}
}

func (s *rowcountState) get(target string) *int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[target]
	if !ok {
		return nil
	}
	return &v
}

func (s *rowcountState) set(target string, value *int64) {
	if value == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[target] = *value
}
