//go:build linux

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/sandboxfs"
)

func TestIsSandboxReview(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"review selected", []string{"tai", "sandbox"}, true},
		{"review after flags", []string{"tai", "-file", "foo", "sandbox"}, true},
		{"generation session", []string{"tai", "ai"}, false},
		{"no command", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSandboxReview(c.args); got != c.want {
				t.Fatalf("isSandboxReview(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestSetupSandboxRootSkippedForReview(t *testing.T) {
	t.Setenv("CAI_DISABLE_SANDBOX_ROOT", "")
	prev := os.Args
	os.Args = []string{"tai", "sandbox"}
	t.Cleanup(func() { os.Args = prev })
	if err := setupSandboxRoot(t.TempDir()); err != nil {
		t.Fatalf("the review invocation must not need the sandbox root: %v", err)
	}
}

// TestCloseSandboxRootWithoutAMount protects TheoryOfSandboxRoot: the
// process's top frame calls the close on every path, so a process without a
// sandbox — a failed mount, the review command, a disabled run — must close
// nothing and must survive a repeated call.
func TestCloseSandboxRootWithoutAMount(t *testing.T) {
	CloseSandboxRoot()
	CloseSandboxRoot()
}

func TestSandboxRecordOptions(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cwd := "/work"
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	// The default is one directory record per session, named by
	// sandboxfs.SessionDirPath. See TheoryOfSandboxRoot.
	record, dirRecord, duckdb, err := sandboxRecordOptions(cwd, env(nil), now)
	if err != nil {
		t.Fatal(err)
	}
	if !dirRecord || duckdb {
		t.Fatalf("default selected dir %v, duckdb %v; want the directory layer", dirRecord, duckdb)
	}
	prefix := filepath.Join(cwd, ".tai-sandboxfs.20250102-030405.")
	if !strings.HasPrefix(record, prefix) {
		t.Fatalf("default record %q, want a session directory with the prefix %q", record, prefix)
	}

	// A named directory pins the place.
	record, dirRecord, duckdb, err = sandboxRecordOptions(cwd, env(map[string]string{
		sandboxDirEnv: "/records/pinned",
	}), now)
	if err != nil || !dirRecord || duckdb || record != "/records/pinned" {
		t.Fatalf("pinned directory: %q dir %v duckdb %v err %v", record, dirRecord, duckdb, err)
	}

	// A named database selects the SQL layer.
	record, dirRecord, duckdb, err = sandboxRecordOptions(cwd, env(map[string]string{
		sandboxDBEnv: "/records/s.db",
	}), now)
	if err != nil || dirRecord || duckdb || record != "/records/s.db" {
		t.Fatalf("database record: %q dir %v duckdb %v err %v", record, dirRecord, duckdb, err)
	}

	// The dialect without a record records itself at the database default.
	record, dirRecord, duckdb, err = sandboxRecordOptions(cwd, env(map[string]string{
		sandboxDuckDBEnv: "1",
	}), now)
	if err != nil || dirRecord || !duckdb || record != sandboxfs.DefaultDBPath(cwd) {
		t.Fatalf("duckdb default: %q dir %v duckdb %v err %v", record, dirRecord, duckdb, err)
	}

	// The two names are exclusive, because a session has one record.
	if _, _, _, err := sandboxRecordOptions(cwd, env(map[string]string{
		sandboxDirEnv: "/a",
		sandboxDBEnv:  "/b",
	}), now); err == nil {
		t.Fatal("two record names were accepted")
	}
	if _, _, _, err := sandboxRecordOptions(cwd, env(map[string]string{
		sandboxDirEnv:    "/a",
		sandboxDuckDBEnv: "1",
	}), now); err == nil {
		t.Fatal("a directory record and a dialect were accepted together")
	}
}
