//go:build linux

package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reusee/tai/sandboxfs"
)

const TheoryOfSandboxRoot = `
The task's filesystem environment is a FUSE mount pivoted to become the
process root inside the mount namespace: every absolute path the process
and its children touch resolves through the sandbox store, which reads
through to the host and records writes. See sandboxfs.TheoryOfFuseMount
for the mechanism and sandboxfs.TheoryOfSandboxFilesystem for the store.

The existing mount policy stays the authority on what may be written. The
store's write policy admits exactly WritableDirs() — the set the mount layer
already makes writable — so the sandbox changes where a write lands, not
whether it is allowed. The policy must stay pure: a policy that performed
filesystem I/O through the mount would wait on the request it is itself
answering, so the worker serving that request could never finish. The
writable set is resolved once, before the pivot, and matched by path prefix.
See sandboxfs.TheoryOfFuseConcurrency and sandboxfs.TheoryOfWritePolicy.

Passthrough directories bypass the store: the kernel filesystems (/dev,
/proc, /sys), /tmp (a fresh tmpfs, not a bind of the container's /tmp, so
the mountpoint never loops back into the view), the Go toolchain caches,
and the user config directory. Their content is machine-managed state whose
interception adds cost without review value, and a read-through proxy
cannot emulate the kernel filesystems at all. The mountpoint lives on the
container's /tmp, so the mount moves with the pivot and leaves no directory
in the project.

The default upper layer is process memory: the session's mutations live in
process memory, the mount leaves no record behind, and the goal runner's
per-loop apply is the only path that reaches the host. The layer pays no
disk round trip per mutation, and the trade-off is explicit: an abandoned
run keeps nothing to review, because the changes it never applied never
left the process, and the sandbox command has no record to read in this mode.
See sandboxfs.TheoryOfUpperLayer.

A durable session is opt-in, and its record takes one of two shapes.
CAI_SANDBOX_DB names a database record, and CAI_SANDBOX_DUCKDB selects that
record's DuckDB dialect instead of sqlite's; a dialect named without a record
records itself at sandboxfs.DefaultDBPath of the working directory — one
hidden file beside the project, browsable and appliable after the session by
the sandbox command. CAI_SANDBOX_DIR names a directory record instead: one
hidden directory beside the project holding ordinary files, which needs no
driver and no schema (sandboxfs.TheoryOfDirRecord). The two names are
exclusive, because a session has one record; naming both fails the mount. A
DuckDB session's binary must link a DuckDB driver: none is linked here, so a
session that selects it without one fails the mount, and the best-effort rule
below leaves the mount policy as the only layer. The store opens the record
before the pivot and holds it for the session's life, because an open after
the pivot would resolve its own path through the mount the store answers. The
CAI_SANDBOX_DB and CAI_SANDBOX_DIR overrides must name a place still visible
at the same path after the pivot: the working directory and every passthrough
qualify; /tmp does not, because the sandbox mounts a fresh tmpfs there and the
original /tmp is hidden.

Each goal loop's changes go back to the host when the loop ends: the store
that served the pivot is recorded as the process's active store, and the
goal runner applies that loop's recorded changes before the next loop reads
the filesystem, so a run's work is durable as it proceeds. A change a host
modification conflicts with stays in the store — the apply skips it and the
runner reports it — so the review command remains the place where the user
resolves what the loops kept back. See pipeline.TheoryOfLoopChangeApply.

The review command is exempt: the sandbox command applies recorded changes
to the host, so a review running inside the sandbox root would write into
the store it reviews instead of the host. The whole environment setup is
skipped for that invocation, leaving the mount policy as the only layer,
which is the set the review may write.

The layer is best-effort like the mount hardening: a kernel without
unprivileged FUSE, a missing /dev/fuse, or a failed pivot leaves the
process with the mount policy alone and the session continues, with no
active store and therefore no per-loop apply.
`

// sandboxDuckDBEnv selects the DuckDB dialect of the durable SQL layer for the
// session record. The binary must link a DuckDB driver; without one the mount
// fails, and the best-effort rule leaves the session on the mount policy alone
// (see sandboxfs.TheoryOfSqlDialects).
const sandboxDuckDBEnv = "CAI_SANDBOX_DUCKDB"

// disableSandboxRootEnv skips the sandbox filesystem, leaving the mount
// policy as the only layer. See TheoryOfSandboxRoot.
const disableSandboxRootEnv = "CAI_DISABLE_SANDBOX_ROOT"

// isSandboxReview reports whether the invocation selects the sandbox review
// command. The review applies recorded changes to the host and must not run
// inside the sandbox root: its writes would reach the store it reviews, in
// a second database layer, and its apply would land there instead of on the
// host. The subcommand is a bare word, so the presence of the word selects
// the review; a false positive — a flag value that happens to be "sandbox"
// — only drops the environment to the mount policy, which is the behavior
// before the sandbox root existed. See TheoryOfSandboxRoot.
func isSandboxReview(args []string) bool {
	for _, a := range args {
		if a == "sandbox" {
			return true
		}
	}
	return false
}

// sandboxDBEnv names the durable sandbox record of this session. Naming it
// turns the default memory layer into a durable one; the dialect variable
// supplies the engine that holds the record. See TheoryOfSandboxRoot.
const sandboxDBEnv = "CAI_SANDBOX_DB"

// sandboxDirEnv names a directory record of this session. Naming it selects the
// directory upper layer (sandboxfs.TheoryOfDirRecord), so the session's changes
// live in ordinary files instead of a database and need no driver. It is
// exclusive with sandboxDBEnv: a session has one record. See
// TheoryOfSandboxRoot.
const sandboxDirEnv = "CAI_SANDBOX_DIR"

// setupSandboxRoot mounts the sandbox filesystem and pivots the process root
// to it, then records the store that served the pivot as the process's active
// store, so the goal runner applies every loop's changes to the host. It runs
// after the container filesystem hardening, so the passthrough sources are the
// hardened mounts. The memory upper layer is the default; CAI_SANDBOX_DB names a
// durable database record instead, CAI_SANDBOX_DUCKDB selects that record's
// DuckDB dialect, and CAI_SANDBOX_DIR names a durable directory record. See
// TheoryOfSandboxRoot and sandboxfs.TheoryOfSqlDialects and
// sandboxfs.TheoryOfDirRecord.
func setupSandboxRoot(cwd string) error {
	if os.Getenv(disableSandboxRootEnv) != "" {
		return nil
	}
	if isSandboxReview(os.Args) {
		return nil
	}
	// Memory is the default: a durable record exists only where the session
	// named one, or named a marker that then records itself at the project's
	// default path. The database and the directory record are exclusive,
	// because a session has one record. See TheoryOfSandboxRoot.
	duckdb := os.Getenv(sandboxDuckDBEnv) != ""
	dirRecord := os.Getenv(sandboxDirEnv) != ""
	dbPath := os.Getenv(sandboxDBEnv)
	if dirRecord {
		if dbPath != "" {
			return fmt.Errorf("%s and %s name one session's record; set one", sandboxDirEnv, sandboxDBEnv)
		}
		if duckdb {
			return fmt.Errorf("%s and %s name one session's record; set one", sandboxDirEnv, sandboxDuckDBEnv)
		}
		dbPath = os.Getenv(sandboxDirEnv)
	}
	if dbPath == "" && duckdb {
		dbPath = sandboxfs.DefaultDBPath(cwd)
	}
	var absDB string
	if dbPath != "" {
		var err error
		absDB, err = filepath.Abs(dbPath)
		if err != nil {
			return err
		}
	}
	passthrough := sandboxfs.DefaultPassthrough()
	for _, dir := range filepath.SplitList(os.Getenv(goWritableDirsEnv)) {
		if dir = strings.TrimSpace(dir); dir != "" {
			passthrough = append(passthrough, dir)
		}
	}
	if configDir := os.Getenv(configDirEnv); configDir != "" {
		passthrough = append(passthrough, configDir)
	}
	mountpoint := filepath.Join(os.TempDir(), "tai-sandbox-root")
	mounted, err := sandboxfs.Mount(sandboxfs.MountOptions{
		Base:        "/",
		DBPath:      absDB,
		DuckDB:      duckdb,
		DirRecord:   dirRecord,
		Mountpoint:  mountpoint,
		Passthrough: passthrough,
		WritePolicy: sandboxWritePolicy(WritableDirs()),
		Pivot:       true,
	})
	if err != nil {
		return err
	}
	// The store that served the pivot is the process's filesystem
	// environment, so the goal runner applies every loop's changes through
	// it. See sandboxfs.TheoryOfActiveStore and
	// pipeline.TheoryOfLoopChangeApply.
	sandboxfs.SetActiveStore(mounted.Store)
	return nil
}

// sandboxWritePolicy returns the store's write confinement over the given
// writable directories: a path is admitted when it is one of them or lies
// beneath one. The comparison is textual by design; see
// TheoryOfSandboxRoot for why the policy performs no filesystem I/O.
func sandboxWritePolicy(dirs []string) func(string) error {
	return func(p string) error {
		for _, dir := range dirs {
			if dir == "" || dir == "/" {
				continue
			}
			if p == dir || strings.HasPrefix(p, dir+"/") {
				return nil
			}
		}
		return fmt.Errorf("%w: %s is outside the writable directories", os.ErrPermission, p)
	}
}
