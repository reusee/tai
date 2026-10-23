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
whether it is allowed. The policy must stay pure: the FUSE server answers
requests on one goroutine, so a policy that performed filesystem I/O
through the mount would wait for the server it is running on. The writable
set is resolved once, before the pivot, and matched by path prefix.

Passthrough directories bypass the store: the kernel filesystems (/dev,
/proc, /sys), /tmp (a fresh tmpfs, not a bind of the container's /tmp, so
the mountpoint never loops back into the view), the Go toolchain caches,
and the user config directory. Their content is machine-managed state whose
interception adds cost without review value, and a read-through proxy
cannot emulate the kernel filesystems at all. The mountpoint lives on the
container's /tmp, so the mount moves with the pivot and leaves no directory
in the project.

The sqlite file lies at sandboxfs.DefaultDBPath of the working directory:
one hidden file beside the project, browsable and appliable after the
session by the sandbox command. The store opens it before the pivot and
holds one connection for the session's life, because a connection opened
later would resolve its own path through the mount the store answers. The
CAI_SANDBOX_DB override must name a directory still visible at the same
path after the pivot: the working directory and every passthrough qualify;
/tmp does not, because the sandbox mounts a fresh tmpfs there and the
original /tmp is hidden.

The review command is exempt: the sandbox command applies recorded changes
to the host, so a review running inside the sandbox root would write into
the store it reviews instead of the host. The whole environment setup is
skipped for that invocation, leaving the mount policy as the only layer,
which is the set the review may write.

The layer is best-effort like the mount hardening: a kernel without
unprivileged FUSE, a missing /dev/fuse, or a failed pivot leaves the
process with the mount policy alone and the session continues. Applying
the recorded changes back to the host stays the user's decision; see
sandboxfs.TheoryOfApplyConflict.
`

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

// sandboxDBEnv overrides the sandbox database path of this session. The
// default is sandboxfs.DefaultDBPath of the working directory.
// See TheoryOfSandboxRoot.
const sandboxDBEnv = "CAI_SANDBOX_DB"

// setupSandboxRoot mounts the sandbox filesystem and pivots the process
// root to it. It runs after the container filesystem hardening, so the
// passthrough sources are the hardened mounts. See TheoryOfSandboxRoot.
func setupSandboxRoot(cwd string) error {
	if os.Getenv(disableSandboxRootEnv) != "" {
		return nil
	}
	if isSandboxReview(os.Args) {
		return nil
	}
	dbPath := os.Getenv(sandboxDBEnv)
	if dbPath == "" {
		dbPath = sandboxfs.DefaultDBPath(cwd)
	}
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		return err
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
	_, err = sandboxfs.Mount(sandboxfs.MountOptions{
		Base:        "/",
		DBPath:      absDB,
		Mountpoint:  mountpoint,
		Passthrough: passthrough,
		WritePolicy: sandboxWritePolicy(WritableDirs()),
		Pivot:       true,
	})
	return err
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
