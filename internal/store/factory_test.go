package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactory_SQLiteDefault(t *testing.T) {
	tmpDir := t.TempDir()
	original := os.Getenv("APPDATA")
	t.Setenv("APPDATA", tmpDir)
	defer os.Setenv("APPDATA", original)

	s, err := New(StoreConfig{})
	if err != nil {
		t.Fatalf("New with empty config: %v", err)
	}
	defer s.Close()

	// Should default to sqlite and succeed.
	count, err := s.TranscriptionCount(context.Background())
	if err != nil {
		t.Fatalf("TranscriptionCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestFactory_ExplicitSQLite(t *testing.T) {
	tmpPath := filepath.Join(t.TempDir(), "explicit.db")
	s, err := New(StoreConfig{Backend: "sqlite", SQLitePath: tmpPath})
	if err != nil {
		t.Fatalf("New with explicit sqlite: %v", err)
	}
	defer s.Close()

	if _, statErr := os.Stat(tmpPath); os.IsNotExist(statErr) {
		t.Fatalf("expected database file at %s", tmpPath)
	}
}

func TestFactory_PostgresRequiresDSN(t *testing.T) {
	_, err := New(StoreConfig{Backend: "postgres"})
	if err == nil {
		t.Fatal("expected error for postgres backend without DSN")
	}
	if !strings.Contains(err.Error(), "requires a DSN") {
		t.Fatalf("error = %q, want message about missing DSN", err.Error())
	}
}

func TestFactory_PostgresAttemptsRealConnection(t *testing.T) {
	_, err := New(StoreConfig{
		Backend:     "postgres",
		PostgresDSN: "postgres://127.0.0.1:1/speechkit?sslmode=disable&connect_timeout=1",
	})
	if err == nil {
		t.Fatal("expected connection error for unreachable postgres endpoint")
	}
	if strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("error = %q, want real connection failure instead of stub message", err.Error())
	}
}

func TestFactory_UnknownBackend(t *testing.T) {
	_, err := New(StoreConfig{Backend: "foobar"})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %q, want message containing 'unknown'", err.Error())
	}
}

func TestRegisterBackend(t *testing.T) {
	RegisterBackend("test", func(cfg StoreConfig) (Store, error) {
		return &mockStore{}, nil
	})
	defer delete(registeredBackends, "test")

	s, err := New(StoreConfig{Backend: "test"})
	if err != nil {
		t.Fatalf("New with registered backend: %v", err)
	}
	defer s.Close()

	if _, ok := s.(*mockStore); !ok {
		t.Fatalf("expected *mockStore, got %T", s)
	}
}
