package agentkit

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Memory is a swappable session-scoped key-value store. The default
// implementation is in-memory; consumers may provide Postgres-, Redis-, or
// vector-backed implementations.
//
// Convention: keys are namespaced by session id using the prefix
// "{sessionID}/" so that List(sessionID + "/") yields all keys for one
// session. The InMemory implementation does not enforce this; it is a
// caller-side convention.
type Memory interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
}

// NewInMemory returns a thread-safe in-memory Memory.
func NewInMemory() Memory {
	return &inMemory{store: make(map[string]string)}
}

type inMemory struct {
	mu    sync.RWMutex
	store map[string]string
}

func (m *inMemory) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.store[key]
	return v, ok, nil
}

func (m *inMemory) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = value
	return nil
}

func (m *inMemory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (m *inMemory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.store, key)
	return nil
}
