//go:build linux

package persona

import "context"

// Persister is the optional durability contract the Registry uses to persist
// admin-authored changes. Production deployments satisfy this via the
// SQLite-backed store (internal/server/persona/sqlite_persister.go); unit
// tests can supply an in-memory fake.
//
// The interface is deliberately coarse — one Save/Delete per entity —
// because the Registry itself is the source of truth for validation and
// ordering. The Persister only has to round-trip bytes.
type Persister interface {
	SavePersona(ctx context.Context, p Persona) error
	DeletePersona(ctx context.Context, id string) error
	LoadPersonas(ctx context.Context) ([]Persona, error)

	SaveRole(ctx context.Context, r Role) error
	DeleteRole(ctx context.Context, id string) error
	LoadRoles(ctx context.Context) ([]Role, error)

	SaveSequence(ctx context.Context, s Sequence) error
	DeleteSequence(ctx context.Context, id string) error
	LoadSequences(ctx context.Context) ([]Sequence, error)
}

// WithPersister attaches a Persister to the Registry. Calls to
// UpsertPersona / UpsertRole / UpsertSequence (and their Delete siblings)
// will now round-trip through the persister after the in-memory mutation
// succeeds. Hydration is explicit — call HydrateFrom once at startup.
func (r *Registry) WithPersister(p Persister) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.persister = p
	return r
}

// HydrateFrom populates the registry with entries previously persisted by
// the given Persister. Entries are tagged Source="store" so they can be
// distinguished from TOML seeds in the API output.
//
// This is called from the bootstrap BEFORE TOML seeds are loaded, then the
// seeds overlay the stored entries — TOML seeds act as defaults that a
// persisted override can replace. If you want TOML to win, invert the
// order in bootstrap.
func (r *Registry) HydrateFrom(ctx context.Context, p Persister) error {
	if p == nil {
		return nil
	}
	personas, err := p.LoadPersonas(ctx)
	if err != nil {
		return err
	}
	roles, err := p.LoadRoles(ctx)
	if err != nil {
		return err
	}
	sequences, err := p.LoadSequences(ctx)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range personas {
		copied := item
		copied.Source = "store"
		r.personas[copied.ID] = &copied
	}
	for _, item := range roles {
		copied := item
		copied.Source = "store"
		r.roles[copied.ID] = &copied
	}
	for _, item := range sequences {
		copied := item
		copied.Source = "store"
		r.sequences[copied.ID] = &copied
	}
	return nil
}
