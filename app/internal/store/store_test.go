package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCloseIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First close should succeed.
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second close must not panic. An error is acceptable (sql: database is
	// closed) but a panic is not.
	_ = s.Close()
}

func TestNewWithEmptyPath(t *testing.T) {
	// Point both profile directories at temp dirs so the default path lands
	// somewhere safe. User content defaults to Local AppData on Windows and
	// to the roaming/config directory elsewhere.
	roaming, local := t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", roaming)
	t.Setenv("LOCALAPPDATA", local)

	s, err := New(StoreConfig{Backend: "sqlite", MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New with empty path: %v", err)
	}
	defer s.Close()

	root := roaming
	if runtime.GOOS == "windows" {
		root = local
	}
	if _, err := os.Stat(filepath.Join(root, "SpeechKit", "feedback.db")); err != nil {
		t.Errorf("expected the database under %s: %v", root, err)
	}
}

func TestSQLiteStoreProvidesSemanticCapabilities(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	provider, ok := s.(SemanticCapabilityProvider)
	if !ok {
		t.Fatal("sqlite store should expose semantic capabilities")
	}

	caps := provider.SemanticCapabilities(context.Background())
	if caps.Embeddings {
		t.Fatal("sqlite local mode should not advertise embeddings by default")
	}
	if caps.VectorSearch {
		t.Fatal("sqlite local mode should not advertise vector search by default")
	}
	if caps.Provider != SemanticProviderNone {
		t.Fatalf("semantic provider = %q, want %q", caps.Provider, SemanticProviderNone)
	}
}

func TestStoreInterface_CompileCheck(t *testing.T) {
	// Compile-time check: SQLiteStore must implement Store.
	var _ Store = (*SQLiteStore)(nil)
}
