package export

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// Registry holds the targets a host offers. It is safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	targets map[string]Target
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{targets: map[string]Target{}}
}

// Register adds a target. A second target with the same id is refused.
func (r *Registry) Register(target Target) error {
	if target == nil {
		return fmt.Errorf("export: register a nil target")
	}
	id := strings.TrimSpace(target.ID())
	if id == "" {
		return fmt.Errorf("export: register a target without an id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.targets[id]; exists {
		return fmt.Errorf("export: target %q already registered", id)
	}
	r.targets[id] = target
	return nil
}

// Target returns the registered target, or ErrTargetUnavailable.
func (r *Registry) Target(id string) (Target, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	target, ok := r.targets[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTargetUnavailable, id)
	}
	return target, nil
}

// IDs lists the registered target ids in sorted order.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.targets))
	for id := range r.targets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Push looks the target up and pushes the document to it.
func (r *Registry) Push(ctx context.Context, targetID string, doc Document, opts Options) (Receipt, error) {
	target, err := r.Target(targetID)
	if err != nil {
		return Receipt{}, err
	}
	return target.Push(ctx, doc, opts)
}
