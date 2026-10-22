//go:build linux

package persona

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/store"
	_ "modernc.org/sqlite"
)

// Persisted schema upgrade: a database written with the legacy persona schema is
// repaired by the store on open, and the persister works on the result.

func TestSQLitePersisterWorksAfterStoreRepairsLegacyPersonaSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
CREATE TABLE voice_agent_personas (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    voice TEXT NOT NULL DEFAULT '',
    locale TEXT NOT NULL DEFAULT '',
    default_role TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_roles (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE voice_agent_sequences (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    steps_json TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy persona schema: %v", err)
	}
	_ = db.Close()

	sqliteStore, err := store.NewSQLiteStore(store.StoreConfig{SQLitePath: dbPath})
	if err != nil {
		t.Fatalf("NewSQLiteStore should repair legacy persona schema: %v", err)
	}
	defer sqliteStore.Close()

	p := NewSQLitePersister(sqliteStore.DB())
	ctx := context.Background()
	if err := p.SavePersona(ctx, Persona{ID: "p", DisplayName: "Persona", DefaultSequence: "seq"}); err != nil {
		t.Fatalf("SavePersona after repair: %v", err)
	}
	if err := p.SaveRole(ctx, Role{ID: "r", DisplayName: "Role", SystemPrompt: "help", ToolAllowlist: []string{"clipboard.read"}}); err != nil {
		t.Fatalf("SaveRole after repair: %v", err)
	}
	if err := p.SaveSequence(ctx, Sequence{
		ID:          "seq",
		DisplayName: "Sequence",
		Completion:  "explicit_close",
		Steps:       []SequenceStep{{ID: "one", Instruction: "start"}},
	}); err != nil {
		t.Fatalf("SaveSequence after repair: %v", err)
	}
	if personas, err := p.LoadPersonas(ctx); err != nil || len(personas) != 1 || personas[0].DefaultSequence != "seq" {
		t.Fatalf("LoadPersonas after repair = %+v, %v", personas, err)
	}
	if roles, err := p.LoadRoles(ctx); err != nil || len(roles) != 1 || len(roles[0].ToolAllowlist) != 1 {
		t.Fatalf("LoadRoles after repair = %+v, %v", roles, err)
	}
	if sequences, err := p.LoadSequences(ctx); err != nil || len(sequences) != 1 || sequences[0].Completion != "explicit_close" {
		t.Fatalf("LoadSequences after repair = %+v, %v", sequences, err)
	}
}
