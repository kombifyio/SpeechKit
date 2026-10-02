package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// newTestSQLiteStoreForActivations spins up a fresh in-process SQLite
// store using the same constructor production uses. Each test gets
// an isolated db file under t.TempDir() so parallel runs don't
// share state.
func newTestSQLiteStoreForActivations(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := New(StoreConfig{
		Backend:    "sqlite",
		SQLitePath: path,
	})
	if err != nil {
		t.Fatalf("New(sqlite): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s, ok := store.(*SQLiteStore)
	if !ok {
		t.Fatalf("expected *SQLiteStore, got %T", store)
	}
	return s
}

func sampleActivation(id, userID, orgID string) WakewordActivation {
	return WakewordActivation{
		ID:           id,
		OwnerUserID:  userID,
		OwnerOrgID:   orgID,
		PhraseID:     "hey_quby",
		Phrase:       "Hey Quby",
		Backend:      "livekit_openwakeword",
		Score:        0.87,
		CapturedAt:   time.Date(2026, 5, 21, 14, 7, 3, 0, time.UTC),
		AudioPath:    "alice/" + id + ".wav",
		AudioBytes:   24044,
		SampleRate:   16000,
		PreRollMs:    1500,
		PostRollMs:   500,
		MetadataJSON: `{"version":"v0.37.5"}`,
	}
}

func TestWakewordActivation_SaveAndGet(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()

	saved, err := s.SaveWakewordActivation(ctx, sampleActivation("a1", "alice", "kombify"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID != "a1" {
		t.Errorf("Save returned ID = %q", saved.ID)
	}
	if saved.UploadedAt.IsZero() {
		t.Error("expected UploadedAt to be populated by store")
	}

	got, err := s.GetWakewordActivation(ctx, "a1", "alice", "kombify")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PhraseID != "hey_quby" || got.Phrase != "Hey Quby" {
		t.Errorf("Get returned phrase mismatch: %+v", got)
	}
	if got.Score != 0.87 {
		t.Errorf("Score = %v", got.Score)
	}
	if got.AudioBytes != 24044 || got.SampleRate != 16000 {
		t.Errorf("audio metadata mismatch: %+v", got)
	}
}

func TestWakewordActivation_GetScopedToOwner(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()
	if _, err := s.SaveWakewordActivation(ctx, sampleActivation("a1", "alice", "kombify")); err != nil {
		t.Fatalf("Save(alice): %v", err)
	}
	// Bob can't see Alice's activation even with the right ID.
	_, err := s.GetWakewordActivation(ctx, "a1", "bob", "kombify")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows when reading across owner, got %v", err)
	}
	// Cross-org isolation: different org, same user — also no leak.
	_, err = s.GetWakewordActivation(ctx, "a1", "alice", "other-org")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows across org, got %v", err)
	}
}

func TestWakewordActivation_RejectsMissingRequiredFields(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()

	cases := []WakewordActivation{
		{OwnerUserID: "a", OwnerOrgID: "k", AudioPath: "x"},          // missing ID
		{ID: "x", OwnerOrgID: "k", AudioPath: "x"},                   // missing user
		{ID: "x", OwnerUserID: "a", AudioPath: "x"},                  // missing org
		{ID: "x", OwnerUserID: "a", OwnerOrgID: "k"},                 // missing audio_path
		{ID: " ", OwnerUserID: "a", OwnerOrgID: "k", AudioPath: "x"}, // whitespace-only id
	}
	for i, c := range cases {
		_, err := s.SaveWakewordActivation(ctx, c)
		if !errors.Is(err, ErrInvalidWakewordActivation) {
			t.Errorf("case %d: expected ErrInvalidWakewordActivation, got %v", i, err)
		}
	}
}

func TestWakewordActivation_ListNewestFirst(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()
	older := sampleActivation("old", "alice", "kombify")
	older.CapturedAt = time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	newer := sampleActivation("new", "alice", "kombify")
	newer.CapturedAt = time.Date(2026, 5, 21, 14, 7, 3, 0, time.UTC)

	if _, err := s.SaveWakewordActivation(ctx, older); err != nil {
		t.Fatalf("Save older: %v", err)
	}
	if _, err := s.SaveWakewordActivation(ctx, newer); err != nil {
		t.Fatalf("Save newer: %v", err)
	}

	list, err := s.ListWakewordActivations(ctx, "alice", "kombify", ListOpts{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if list[0].ID != "new" || list[1].ID != "old" {
		t.Errorf("ordering broken: %v", []string{list[0].ID, list[1].ID})
	}
}

func TestWakewordActivation_ListScopedToOwner(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()
	if _, err := s.SaveWakewordActivation(ctx, sampleActivation("a1", "alice", "kombify")); err != nil {
		t.Fatalf("Save(alice): %v", err)
	}
	if _, err := s.SaveWakewordActivation(ctx, sampleActivation("b1", "bob", "kombify")); err != nil {
		t.Fatalf("Save(bob): %v", err)
	}

	alice, err := s.ListWakewordActivations(ctx, "alice", "kombify", ListOpts{})
	if err != nil {
		t.Fatalf("List(alice): %v", err)
	}
	if len(alice) != 1 || alice[0].ID != "a1" {
		t.Errorf("alice should see only a1, got %v", alice)
	}
	bob, err := s.ListWakewordActivations(ctx, "bob", "kombify", ListOpts{})
	if err != nil {
		t.Fatalf("List(bob): %v", err)
	}
	if len(bob) != 1 || bob[0].ID != "b1" {
		t.Errorf("bob should see only b1, got %v", bob)
	}
}

func TestWakewordActivation_LabelUpdate(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()
	if _, err := s.SaveWakewordActivation(ctx, sampleActivation("a1", "alice", "kombify")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Initial state: unlabeled.
	got, _ := s.GetWakewordActivation(ctx, "a1", "alice", "kombify")
	if got.Label != "" {
		t.Errorf("expected empty initial label, got %q", got.Label)
	}

	// Apply "correct".
	if err := s.UpdateWakewordActivationLabel(ctx, "a1", "alice", "kombify", WakewordLabelCorrect); err != nil {
		t.Fatalf("UpdateLabel: %v", err)
	}
	got, _ = s.GetWakewordActivation(ctx, "a1", "alice", "kombify")
	if got.Label != WakewordLabelCorrect {
		t.Errorf("label = %q, want %q", got.Label, WakewordLabelCorrect)
	}

	// Switch to "false_positive".
	if err := s.UpdateWakewordActivationLabel(ctx, "a1", "alice", "kombify", WakewordLabelFalsePositive); err != nil {
		t.Fatalf("UpdateLabel: %v", err)
	}
	got, _ = s.GetWakewordActivation(ctx, "a1", "alice", "kombify")
	if got.Label != WakewordLabelFalsePositive {
		t.Errorf("label = %q, want %q", got.Label, WakewordLabelFalsePositive)
	}

	// Bogus label rejected.
	if err := s.UpdateWakewordActivationLabel(ctx, "a1", "alice", "kombify", "garbage"); err == nil {
		t.Error("expected rejection of bogus label")
	}

	// Cross-owner update silently fails (no row matched).
	if err := s.UpdateWakewordActivationLabel(ctx, "a1", "bob", "kombify", WakewordLabelCorrect); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows across owner, got %v", err)
	}
}

func TestWakewordActivation_DeleteReturnsAudioPath(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()
	a := sampleActivation("a1", "alice", "kombify")
	if _, err := s.SaveWakewordActivation(ctx, a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	audioPath, err := s.DeleteWakewordActivation(ctx, "a1", "alice", "kombify")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if audioPath != a.AudioPath {
		t.Errorf("audio_path returned = %q, want %q", audioPath, a.AudioPath)
	}

	// Row is gone.
	_, err = s.GetWakewordActivation(ctx, "a1", "alice", "kombify")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}

	// Cross-owner delete also returns ErrNoRows.
	if _, err := s.DeleteWakewordActivation(ctx, "a1", "bob", "kombify"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected ErrNoRows across owner, got %v", err)
	}
}

func TestWakewordActivation_CountAndSumBytes(t *testing.T) {
	s := newTestSQLiteStoreForActivations(t)
	ctx := context.Background()

	a := sampleActivation("a1", "alice", "kombify")
	a.AudioBytes = 10_000
	b := sampleActivation("a2", "alice", "kombify")
	b.AudioBytes = 25_000
	c := sampleActivation("c1", "bob", "kombify")
	c.AudioBytes = 9_999

	for _, x := range []WakewordActivation{a, b, c} {
		if _, err := s.SaveWakewordActivation(ctx, x); err != nil {
			t.Fatalf("Save(%s): %v", x.ID, err)
		}
	}

	if n, err := s.CountWakewordActivationsForUser(ctx, "alice", "kombify"); err != nil || n != 2 {
		t.Errorf("Count(alice) = %d, %v", n, err)
	}
	if n, err := s.CountWakewordActivationsForUser(ctx, "bob", "kombify"); err != nil || n != 1 {
		t.Errorf("Count(bob) = %d, %v", n, err)
	}
	if bytes, err := s.SumWakewordActivationBytesForUser(ctx, "alice", "kombify"); err != nil || bytes != 35_000 {
		t.Errorf("Sum(alice) = %d, %v", bytes, err)
	}
	// Owner with no rows gets 0, not an error.
	if bytes, err := s.SumWakewordActivationBytesForUser(ctx, "ghost", "kombify"); err != nil || bytes != 0 {
		t.Errorf("Sum(ghost) = %d, %v", bytes, err)
	}
}

func TestValidWakewordLabel_Matrix(t *testing.T) {
	cases := map[string]bool{
		"":               true,
		"correct":        true,
		"false_positive": true,
		"CORRECT":        false,
		"yes":            false,
		" correct ":      false,
	}
	for label, want := range cases {
		got := ValidWakewordLabel(label)
		if got != want {
			t.Errorf("ValidWakewordLabel(%q) = %v, want %v", label, got, want)
		}
	}
}
