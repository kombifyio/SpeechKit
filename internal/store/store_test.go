package store

import (
	"context"
	"os"
	"path/filepath"
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
	// Point APPDATA to a temp dir so the default path lands somewhere safe.
	tmpDir := t.TempDir()
	original := os.Getenv("APPDATA")
	t.Setenv("APPDATA", tmpDir)
	defer os.Setenv("APPDATA", original)

	s, err := New(StoreConfig{Backend: "sqlite", MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("New with empty path: %v", err)
	}
	defer s.Close()

	// Verify the database was created under the temp APPDATA.
	expectedDir := filepath.Join(tmpDir, "SpeechKit")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected directory %s to exist", expectedDir)
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
