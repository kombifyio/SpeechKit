//go:build linux

package persona

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB returns an in-memory SQLite DB with the persona schema already
// applied. Avoids depending on internal/store so this package stays
// dependency-light.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Re-run 007 inline; in production the store migrator applies it at
	// construction time.
	schema := `
CREATE TABLE voice_agent_personas (
    id             TEXT PRIMARY KEY,
    display_name   TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    voice          TEXT NOT NULL DEFAULT '',
    locale         TEXT NOT NULL DEFAULT '',
    default_role   TEXT NOT NULL DEFAULT '',
    tags_json      TEXT NOT NULL DEFAULT '[]',
    metadata_json  TEXT NOT NULL DEFAULT '{}',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_roles (
    id                              TEXT PRIMARY KEY,
    display_name                    TEXT NOT NULL,
    system_prompt                   TEXT NOT NULL,
    refinement_prompt               TEXT NOT NULL DEFAULT '',
    locale                          TEXT NOT NULL DEFAULT '',
    vocabulary_hint                 TEXT NOT NULL DEFAULT '',
    tool_allowlist_json             TEXT NOT NULL DEFAULT '[]',
    temperature                     REAL NOT NULL DEFAULT 0,
    thinking_enabled                INTEGER NOT NULL DEFAULT 0,
    thinking_level                  TEXT NOT NULL DEFAULT '',
    include_thoughts                INTEGER NOT NULL DEFAULT 0,
    thinking_budget                 INTEGER NOT NULL DEFAULT 0,
    automatic_activity_detection    INTEGER NOT NULL DEFAULT 0,
    vad_start_sensitivity           TEXT NOT NULL DEFAULT '',
    vad_end_sensitivity             TEXT NOT NULL DEFAULT '',
    vad_prefix_padding_ms           INTEGER NOT NULL DEFAULT 0,
    vad_silence_duration_ms         INTEGER NOT NULL DEFAULT 0,
    activity_handling               TEXT NOT NULL DEFAULT '',
    turn_coverage                   TEXT NOT NULL DEFAULT '',
    context_compression_enabled     INTEGER NOT NULL DEFAULT 0,
    context_compression_trigger_tk  INTEGER NOT NULL DEFAULT 0,
    context_compression_target_tk   INTEGER NOT NULL DEFAULT 0,
    enable_affective_dialog         INTEGER NOT NULL DEFAULT 0,
    created_at                      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_sequences (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    completion   TEXT NOT NULL DEFAULT '',
    max_turns    INTEGER NOT NULL DEFAULT 0,
    steps_json   TEXT NOT NULL DEFAULT '[]',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
	`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

func TestSQLitePersister_RoundTripsPersona(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	ctx := context.Background()

	in := Persona{
		ID: "host", DisplayName: "Host",
		Description: "Conversation host", Voice: "Kore", Locale: "de",
		DefaultRole: "discovery",
		Tags:        []string{"sales", "structured"},
		Metadata:    map[string]string{"team": "go-to-market"},
	}
	if err := p.SavePersona(ctx, in); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}

	loaded, err := p.LoadPersonas(ctx)
	if err != nil {
		t.Fatalf("LoadPersonas: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(loaded))
	}
	got := loaded[0]
	if got.ID != "host" || got.Voice != "Kore" || got.Locale != "de" {
		t.Fatalf("basic fields lost: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "sales" || got.Tags[1] != "structured" {
		t.Fatalf("tags lost: %+v", got.Tags)
	}
	if got.Metadata["team"] != "go-to-market" {
		t.Fatalf("metadata lost: %+v", got.Metadata)
	}
}

func TestSQLitePersister_UpsertReplaces(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	ctx := context.Background()

	_ = p.SavePersona(ctx, Persona{ID: "x", DisplayName: "Old"})
	if err := p.SavePersona(ctx, Persona{ID: "x", DisplayName: "New"}); err != nil {
		t.Fatalf("SavePersona upsert: %v", err)
	}
	loaded, _ := p.LoadPersonas(ctx)
	if len(loaded) != 1 {
		t.Fatalf("upsert should leave 1 row; got %d", len(loaded))
	}
	if loaded[0].DisplayName != "New" {
		t.Fatalf("upsert did not replace display_name; got %q", loaded[0].DisplayName)
	}
}

func TestSQLitePersister_DeletePersona(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	ctx := context.Background()
	_ = p.SavePersona(ctx, Persona{ID: "x", DisplayName: "X"})
	if err := p.DeletePersona(ctx, "x"); err != nil {
		t.Fatalf("DeletePersona: %v", err)
	}
	loaded, _ := p.LoadPersonas(ctx)
	if len(loaded) != 0 {
		t.Fatalf("expected empty after delete; got %d", len(loaded))
	}
}

func TestSQLitePersister_RoundTripsRole(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	ctx := context.Background()
	in := Role{
		ID: "discovery", DisplayName: "Discovery",
		SystemPrompt: "be nice", RefinementPrompt: "...",
		ToolAllowlist:              []string{"clipboard.read"},
		Temperature:                0.4,
		AutomaticActivityDetection: true,
		VADStartSensitivity:        "medium", VADEndSensitivity: "low",
		VADPrefixPaddingMs: 100, VADSilenceDurationMs: 500,
		ContextCompressionEnabled:   true,
		ContextCompressionTriggerTk: 10000, ContextCompressionTargetTk: 4000,
	}
	if err := p.SaveRole(ctx, in); err != nil {
		t.Fatalf("SaveRole: %v", err)
	}
	loaded, err := p.LoadRoles(ctx)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 role, got %d", len(loaded))
	}
	got := loaded[0]
	if got.ID != "discovery" || got.SystemPrompt != "be nice" {
		t.Fatalf("role basics lost: %+v", got)
	}
	if !got.AutomaticActivityDetection || !got.ContextCompressionEnabled {
		t.Fatalf("bool fields lost: %+v", got)
	}
	if got.VADPrefixPaddingMs != 100 || got.ContextCompressionTriggerTk != 10000 {
		t.Fatalf("int fields lost: %+v", got)
	}
	if len(got.ToolAllowlist) != 1 || got.ToolAllowlist[0] != "clipboard.read" {
		t.Fatalf("tool allowlist lost: %+v", got.ToolAllowlist)
	}
}

func TestSQLitePersister_RoundTripsSequence(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	ctx := context.Background()
	in := Sequence{
		ID: "s", DisplayName: "S", Description: "discovery",
		Completion: "explicit_close",
		Steps: []SequenceStep{
			{ID: "greet", Instruction: "say hi"},
			{ID: "ask", Instruction: "ask needs", RequireTools: []string{"notes"}},
		},
	}
	if err := p.SaveSequence(ctx, in); err != nil {
		t.Fatalf("SaveSequence: %v", err)
	}
	loaded, err := p.LoadSequences(ctx)
	if err != nil {
		t.Fatalf("LoadSequences: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 seq, got %d", len(loaded))
	}
	got := loaded[0]
	if got.ID != "s" || got.Completion != "explicit_close" || len(got.Steps) != 2 {
		t.Fatalf("sequence basics lost: %+v", got)
	}
	if got.Steps[1].ID != "ask" || len(got.Steps[1].RequireTools) != 1 {
		t.Fatalf("steps lost: %+v", got.Steps)
	}
}

func TestRegistry_WithPersister_SavesOnUpsert(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	reg := NewRegistry().WithPersister(p)

	if _, err := reg.UpsertPersona(Persona{ID: "p", DisplayName: "P"}); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}
	loaded, _ := p.LoadPersonas(context.Background())
	if len(loaded) != 1 {
		t.Fatalf("persister should have received the write; got %d rows", len(loaded))
	}
}

func TestRegistry_WithPersister_HydratesOnStart(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	// Pre-populate persister directly.
	_ = p.SavePersona(context.Background(), Persona{ID: "preseed", DisplayName: "Preseed"})
	_ = p.SaveRole(context.Background(), Role{ID: "r1", DisplayName: "R1", SystemPrompt: "..."})
	_ = p.SaveSequence(context.Background(), Sequence{
		ID: "s1", DisplayName: "S1",
		Steps: []SequenceStep{{ID: "only", Instruction: "only"}},
	})

	reg := NewRegistry()
	if err := reg.HydrateFrom(context.Background(), p); err != nil {
		t.Fatalf("HydrateFrom: %v", err)
	}
	if len(reg.ListPersonas()) != 1 || reg.ListPersonas()[0].ID != "preseed" {
		t.Fatalf("hydrated personas missing: %+v", reg.ListPersonas())
	}
	if len(reg.ListRoles()) != 1 || reg.ListRoles()[0].ID != "r1" {
		t.Fatalf("hydrated roles missing: %+v", reg.ListRoles())
	}
	if len(reg.ListSequences()) != 1 || reg.ListSequences()[0].ID != "s1" {
		t.Fatalf("hydrated sequences missing: %+v", reg.ListSequences())
	}
	// Source flag should be "store".
	if reg.ListPersonas()[0].Source != "store" {
		t.Fatalf("hydrated persona Source should be 'store'; got %q", reg.ListPersonas()[0].Source)
	}
}

func TestRegistry_WithPersister_DeletePropagates(t *testing.T) {
	p := NewSQLitePersister(newTestDB(t))
	reg := NewRegistry().WithPersister(p)
	_, _ = reg.UpsertPersona(Persona{ID: "x", DisplayName: "X"})
	if err := reg.DeletePersona("x"); err != nil {
		t.Fatalf("DeletePersona: %v", err)
	}
	loaded, _ := p.LoadPersonas(context.Background())
	if len(loaded) != 0 {
		t.Fatalf("persister should have received the delete; got %d rows", len(loaded))
	}
}

func TestRegistry_WithPersister_TOMLSourceNotPersisted(t *testing.T) {
	// Seeds (Source="toml") must never round-trip to the store — the store
	// is only for admin-authored overrides.
	p := NewSQLitePersister(newTestDB(t))
	reg := NewRegistry().WithPersister(p)
	_, _ = reg.UpsertPersona(Persona{ID: "seed", DisplayName: "Seed", Source: "toml"})
	loaded, _ := p.LoadPersonas(context.Background())
	if len(loaded) != 0 {
		t.Fatalf("toml-sourced entries must not hit the persister; got %d rows", len(loaded))
	}
}

// failingPersister always returns an error from Save/Delete; used to
// verify T22 — that registry mutations refuse to commit in-memory when
// the durable layer rejects them.
type failingPersister struct{ err error }

func (f *failingPersister) SavePersona(ctx context.Context, _ Persona) error  { return f.err }
func (f *failingPersister) DeletePersona(ctx context.Context, _ string) error { return f.err }
func (f *failingPersister) LoadPersonas(ctx context.Context) ([]Persona, error) {
	return nil, nil
}
func (f *failingPersister) SaveRole(ctx context.Context, _ Role) error         { return f.err }
func (f *failingPersister) DeleteRole(ctx context.Context, _ string) error     { return f.err }
func (f *failingPersister) LoadRoles(ctx context.Context) ([]Role, error)      { return nil, nil }
func (f *failingPersister) SaveSequence(ctx context.Context, _ Sequence) error { return f.err }
func (f *failingPersister) DeleteSequence(ctx context.Context, _ string) error { return f.err }
func (f *failingPersister) LoadSequences(ctx context.Context) ([]Sequence, error) {
	return nil, nil
}

func TestRegistry_PersistError_PropagatesAndRefusesCommit(t *testing.T) {
	p := &failingPersister{err: errSentinel}
	reg := NewRegistry().WithPersister(p)

	got, err := reg.UpsertPersona(Persona{ID: "x", DisplayName: "X"})
	if err == nil {
		t.Fatalf("expected upsert to return an error when persister fails")
	}
	if !errors.Is(err, ErrPersist) {
		t.Fatalf("error must wrap ErrPersist: %v", err)
	}
	if !errors.Is(err, errSentinel) {
		t.Fatalf("error must also wrap the underlying persister error: %v", err)
	}
	if got.ID != "" {
		t.Fatalf("returned persona must be zero on failure; got %+v", got)
	}
	// In-memory state must not be polluted.
	if len(reg.ListPersonas()) != 0 {
		t.Fatalf("registry must not commit when persister fails; got %d entries", len(reg.ListPersonas()))
	}
}

func TestRegistry_PersistError_RoleAndSequence(t *testing.T) {
	p := &failingPersister{err: errSentinel}
	reg := NewRegistry().WithPersister(p)

	if _, err := reg.UpsertRole(Role{ID: "r", DisplayName: "R", SystemPrompt: "..."}); !errors.Is(err, ErrPersist) {
		t.Fatalf("role: expected ErrPersist, got %v", err)
	}
	if len(reg.ListRoles()) != 0 {
		t.Fatalf("role must not commit when persister fails")
	}

	seq := Sequence{ID: "s", DisplayName: "S", Steps: []SequenceStep{{ID: "a", Instruction: "a"}}}
	if _, err := reg.UpsertSequence(seq); !errors.Is(err, ErrPersist) {
		t.Fatalf("sequence: expected ErrPersist, got %v", err)
	}
	if len(reg.ListSequences()) != 0 {
		t.Fatalf("sequence must not commit when persister fails")
	}
}

func TestRegistry_DeletePersistError_KeepsInMemory(t *testing.T) {
	good := NewSQLitePersister(newTestDB(t))
	reg := NewRegistry().WithPersister(good)
	_, _ = reg.UpsertPersona(Persona{ID: "x", DisplayName: "X"})

	// Swap in a failing persister and try to delete; in-memory entry
	// must remain so the registry stays consistent with storage.
	reg.WithPersister(&failingPersister{err: errSentinel})
	if err := reg.DeletePersona("x"); !errors.Is(err, ErrPersist) {
		t.Fatalf("expected ErrPersist on delete, got %v", err)
	}
	if len(reg.ListPersonas()) != 1 {
		t.Fatalf("delete must keep in-memory entry on persist failure; got %d entries", len(reg.ListPersonas()))
	}
}

var errSentinel = errors.New("test: persister sentinel")
