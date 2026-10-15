package store

import (
	"context"
	"testing"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// Erasing one account must remove its owner-attributed rows and its scoped
// meeting, and leave every other account's data untouched.
func TestDeleteSubjectKeepsOtherAccounts(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()
	alice := speechstorage.Scope{InstallID: "inst", UserID: "alice"}
	bob := speechstorage.Scope{InstallID: "inst", UserID: "bob"}

	seedMeetingForPrivacyTest(t, s, alice, "alice meeting")
	seedMeetingForPrivacyTest(t, s, bob, "bob meeting")
	if err := s.SaveTranscription(WithRecordOwner(ctx, RecordOwner{UserID: "alice"}), "alice words", "en", "p", "m", 10, 1, nil); err != nil {
		t.Fatalf("save alice: %v", err)
	}
	if err := s.SaveTranscription(WithRecordOwner(ctx, RecordOwner{UserID: "bob"}), "bob words", "en", "p", "m", 10, 1, nil); err != nil {
		t.Fatalf("save bob: %v", err)
	}

	if _, err := s.DeleteSubject(ctx, "alice"); err != nil {
		t.Fatalf("DeleteSubject: %v", err)
	}
	// Idempotent redelivery.
	if _, err := s.DeleteSubject(ctx, "alice"); err != nil {
		t.Fatalf("DeleteSubject repeat: %v", err)
	}

	aliceRows, err := s.CountSubject(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for table, n := range aliceRows {
		if n != 0 {
			t.Fatalf("alice still holds %d rows in %s", n, table)
		}
	}
	bobRows, err := s.CountSubject(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	var held int64
	for _, n := range bobRows {
		held += n
	}
	if held == 0 {
		t.Fatal("bob's data was erased with alice's")
	}
	ids, err := s.SubjectIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == "alice" {
			t.Fatal("alice is still a known subject")
		}
	}
}
