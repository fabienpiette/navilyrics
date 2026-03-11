package handlers

import (
	"sync"

	"github.com/user/navilyrics/internal/lyrics"
)

// RunStore manages active batch runs keyed by a unique run ID.
// Each run sends Result values to its channel; the channel is closed when done.
type RunStore struct {
	mu   sync.Mutex
	runs map[string]chan lyrics.Result
}

func newRunStore() *RunStore {
	return &RunStore{runs: make(map[string]chan lyrics.Result)}
}

func (rs *RunStore) create(id string) chan lyrics.Result {
	ch := make(chan lyrics.Result, 64)
	rs.mu.Lock()
	rs.runs[id] = ch
	rs.mu.Unlock()
	return ch
}

func (rs *RunStore) get(id string) (chan lyrics.Result, bool) {
	rs.mu.Lock()
	ch, ok := rs.runs[id]
	rs.mu.Unlock()
	return ch, ok
}

func (rs *RunStore) delete(id string) {
	rs.mu.Lock()
	delete(rs.runs, id)
	rs.mu.Unlock()
}
