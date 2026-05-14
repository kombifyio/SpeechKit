package storage

import "fmt"

type Capabilities struct {
	Scopes       bool `json:"scopes"`
	AudioAssets  bool `json:"audioAssets"`
	FullText     bool `json:"fullText"`
	Embeddings   bool `json:"embeddings"`
	VectorSearch bool `json:"vectorSearch"`
}

type BackendInfo struct {
	Name         string       `json:"name"`
	DisplayName  string       `json:"displayName,omitempty"`
	ScopePolicy  ScopePolicy  `json:"scopePolicy"`
	Capabilities Capabilities `json:"capabilities"`
}

type Factory[T any] func(Config) (T, error)

type Config struct {
	Backend      string
	DefaultScope Scope
	ScopePolicy  ScopePolicy
	Options      map[string]string
}

type Registry[T any] struct {
	factories map[string]Factory[T]
}

func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{factories: map[string]Factory[T]{}}
}

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

func NormalizeBackendName(name string) string {
	return NormalizeIdentifier(name)
}
