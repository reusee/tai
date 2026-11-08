package sandboxfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuditRecordsAFailedOperation protects TheoryOfAuditOutcome: a refused
// mutation leaves a row that names the path and carries the error, because the
// refused path keeps no upper row and the trail is the only record the attempt
// leaves.
func TestAuditRecordsAFailedOperation(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenDirWithPolicy(filepath.Join(dir, "rec.d"), base, func(p string) error {
		return fmt.Errorf("%w: %s", os.ErrPermission, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.WriteFile("/denied", []byte("x"), 0o644); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("got %v, want a permission error", err)
	}
	ops, err := store.Ops()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.Op != "write" || op.Path != "/denied" {
			continue
		}
		found = true
		if op.Result != ResultError {
			t.Errorf("result %q, want %q", op.Result, ResultError)
		}
		if !strings.Contains(op.Error, "permission") {
			t.Errorf("error %q does not carry the refusal", op.Error)
		}
	}
	if !found {
		t.Fatalf("no write row for the refused path: %+v", ops)
	}
	// A refused write is not a change: the path keeps no upper row.
	if changes, err := store.Changes(); err != nil || len(changes) != 0 {
		t.Fatalf("changes %+v (error %v), want none", changes, err)
	}
}

// TestAuditRecordsASuccessfulResult protects TheoryOfAuditOutcome: the row of an
// operation the store carried out says so and carries no error, so a review
// separates a path the session changed from one it tried and failed to change.
func TestAuditRecordsASuccessfulResult(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	ops, err := store.Ops()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.Op != "mkdir" || op.Path != "/d" {
			continue
		}
		found = true
		if op.Result != ResultOK {
			t.Errorf("result %q, want %q", op.Result, ResultOK)
		}
		if op.Error != "" {
			t.Errorf("a successful row carries the error %q", op.Error)
		}
	}
	if !found {
		t.Fatalf("no mkdir row: %+v", ops)
	}
}

// TestAuditRecordsAConflict protects TheoryOfAuditOutcome: an apply a conflict
// blocks reaches the host never and leaves no upper row of its own, so its
// audit row is what tells the review the change was tried and why it waited.
func TestAuditRecordsAConflict(t *testing.T) {
	store, base := newTestStore(t)
	writeHost(t, base, "a.txt", "before")
	if err := store.WriteFile("/a.txt", []byte("session"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The host moves on after the session read it, so applying would discard
	// the outside change. See TheoryOfApplyConflict.
	writeHost(t, base, "a.txt", "outside")
	applied, conflicts, err := store.ApplyAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 || len(conflicts) != 1 {
		t.Fatalf("applied %v, conflicts %v, want one conflict and nothing applied", applied, conflicts)
	}
	ops, err := store.Ops()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.Op != "apply" || op.Path != "/a.txt" {
			continue
		}
		found = true
		if op.Result != ResultError {
			t.Errorf("result %q, want %q", op.Result, ResultError)
		}
		if !strings.Contains(op.Error, "changed") {
			t.Errorf("error %q does not carry the conflict reason", op.Error)
		}
	}
	if !found {
		t.Fatalf("no apply row for the conflicted path: %+v", ops)
	}
}

// TestAuditRecordsADirectoryRenameOnce protects TheoryOfAuditOutcome: one row
// answers one operation, so a directory rename records the rename the caller
// asked for instead of a second row from the private step that carries it out.
func TestAuditRecordsADirectoryRenameOnce(t *testing.T) {
	store, _ := newTestStore(t)
	if err := store.Mkdir("/d", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteFile("/d/f", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Rename("/d", "/e"); err != nil {
		t.Fatal(err)
	}
	ops, err := store.Ops()
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, op := range ops {
		if op.Path != "/d" || !strings.HasPrefix(op.Op, "rename") {
			continue
		}
		rows++
		if op.Op != "rename" {
			t.Errorf("row %q names the private step, want rename", op.Op)
		}
		if op.Detail != "/e" {
			t.Errorf("detail %q, want /e", op.Detail)
		}
		if op.Result != ResultOK {
			t.Errorf("result %q, want %q", op.Result, ResultOK)
		}
	}
	if rows != 1 {
		t.Fatalf("got %d rename row(s) for /d, want 1", rows)
	}
}
