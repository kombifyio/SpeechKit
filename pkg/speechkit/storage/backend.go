// Package storage defines the public storage-backend contract: backend
// capabilities and metadata, install/device/user/tenant scopes with their
// enforcement policies, and the configuration shape hosts use to construct
// a backend.
package storage

import "fmt"

// Capabilities advertises the optional features a backend implements so
// hosts can adapt: per-scope isolation, audio asset storage, full-text
// search, stored embeddings, and vector similarity search.
type Capabilities struct {
	Scopes       bool `json:"scopes"`
	AudioAssets  bool `json:"audioAssets"`
	FullText     bool `json:"fullText"`
	Embeddings   bool `json:"embeddings"`
	VectorSearch bool `json:"vectorSearch"`
}

// BackendInfo describes a registered backend: the Name it registers under,
// an optional DisplayName for settings surfaces, the [ScopePolicy] it
// enforces, and its [Capabilities].
type BackendInfo struct {
	Name         string       `json:"name"`
	DisplayName  string       `json:"displayName,omitempty"`
	ScopePolicy  ScopePolicy  `json:"scopePolicy"`
	Capabilities Capabilities `json:"capabilities"`
}

// Factory constructs a backend of type T from a [Config].
type Factory[T any] func(Config) (T, error)

// Config is what a host passes to a [Factory]: the registered Backend name,
// the DefaultScope used when a context carries none, the ScopePolicy the
// backend must enforce, and free-form backend-specific Options such as a
// path or DSN.
type Config struct {
	Backend      string
	DefaultScope Scope
	ScopePolicy  ScopePolicy
	Options      map[string]string
}

// Registry maps normalized backend names to factories for one backend type
// T. The zero value is ready to use; registration is not safe for
// concurrent use.
type Registry[T any] struct {
	factories map[string]Factory[T]
}

// NewRegistry returns an empty [Registry] for backend type T.
func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{factories: map[string]Factory[T]{}}
}

// RegisterBackend stores factory under the normalized form of name,
// replacing any earlier registration. It returns an error when the name
// normalizes to "" or factory is nil.
func (r *Registry[T]) RegisterBackend(name string, factory Factory[T]) error {
	if r.factories == nil {
		r.factories = map[string]Factory[T]{}
	}
	name = NormalizeBackendName(name)
	if name == "" {
		return fmt.Errorf("speechkit storage: backend name is required")
	}
	if factory == nil {
		return fmt.Errorf("speechkit storage: backend factory %q is nil", name)
	}
	r.factories[name] = factory
	return nil
}

// NormalizeBackendName canonicalizes a backend name with
// [NormalizeIdentifier] so registrations and lookups agree.
func NormalizeBackendName(name string) string {
	return NormalizeIdentifier(name)
}
