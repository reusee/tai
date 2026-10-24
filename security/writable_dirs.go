package security

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const TheoryOfWritableDirs = `
The writable directories are the set of paths that the container filesystem
makes writable: the current working directory (bind-mounted read-write),
Go toolchain directories (GOCACHE, GOMODCACHE, GOPATH/pkg), the user config
directory, /tmp, and /dev/shm. The focus file check
(pathutil.IsOutsideWritableDirs) and the symlink read-only annotation
(anytexts.isOutsideWritableDirs) both delegate to security.IsWritablePath,
which checks against this same set. This ensures the file collection check
is consistent with the security package's container isolation — no more
and no less restrictive. A focus file in a writable directory (e.g., /tmp,
where Go's t.TempDir creates test directories) is allowed; a focus file in
a read-only directory is rejected at collection time, surfacing the error
before the model is invoked.

The CWD is not cached because it can change during execution (e.g., tests
that os.Chdir to a temp directory). The static writable dirs (Go toolchain
dirs, config dir, /tmp, /dev/shm) are cached via sync.OnceValue because
they are determined by the environment and do not change during execution.
IsWritablePath resolves the CWD fresh on every call and checks it before
the cached static dirs, ensuring correctness even after CWD changes.

Resolution spawns no subprocess inside the sandbox. The toolchain
directories and the config directory are resolved once, before the mount
restrictions, and the hardened environment carries them (goWritableDirsEnv,
configDirEnv); inside the container the set is read from those values. Two
facts decide this: ` + "`go env`" + ` may not work after the mounts hardened,
and every file a toolchain start touches pays a round trip through the
sandbox server that answers the start's own reads. Outside the container one
'go env -json' invocation supplies the three toolchain values together, so
a start that sets none of the variables pays one toolchain start instead of
three, and a start that sets every variable pays none.

A resolved value enters the set only while it still names an existing
directory: the sandbox write policy matches paths by prefix, so a stale
entry would widen the policy over a path that no longer exists.

The Landlock layer (see TheoryOfLandlock in landlock_linux.go) does not
call WritableDirs: it consumes the pre-namespace-resolved environment
values the mount setup consumed, so the two enforcement planes stay
identical without re-running ` + "`go env`" + ` inside the container.
`

// WritableDirs returns the list of directories that the container
// filesystem setup makes writable. These directories match exactly
// what setupContainerFilesystem in container_linux.go binds read-write:
// the current working directory, Go toolchain directories (GOCACHE,
// GOMODCACHE, GOPATH/pkg), the user config directory, /tmp, and /dev/shm.
// On platforms without container isolation, these directories are still
// the canonical writable set, ensuring the focus file check is consistent
// with the security package's restriction. See TheoryOfContainerIsolation
// in container_linux.go and TheoryOfWritableDirs.
func WritableDirs() []string {
	dirs := slices.Clone(cachedStaticWritableDirs())
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	return dirs
}

// IsWritablePath reports whether the given path is within a writable
// directory. A path is writable if it is inside one of the directories
// that the container filesystem makes writable: the current working
// directory, Go toolchain directories (GOCACHE, GOMODCACHE, GOPATH/pkg),
// the user config directory, /tmp, or /dev/shm. Both the path and each
// writable directory are canonicalized via filepath.EvalSymlinks before
// comparison, so symlinked components are handled correctly. If the path
// itself cannot be resolved (e.g., it does not exist yet), the original
// path is used. An error is returned only if the current working
// directory cannot be determined or canonicalized.
// See TheoryOfContainerIsolation in container_linux.go and
// TheoryOfWritableDirs.
func IsWritablePath(path string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return false, err
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	// Check CWD first (resolved fresh, not cached, because CWD can
	// change during execution — e.g., tests that os.Chdir).
	if path == cwd || strings.HasPrefix(path, cwd+string(filepath.Separator)) {
		return true, nil
	}

	// Check static writable dirs (cached, do not change during execution).
	for _, wDir := range cachedCanonicalStaticWritableDirs() {
		if path == wDir || strings.HasPrefix(path, wDir+string(filepath.Separator)) {
			return true, nil
		}
	}

	return false, nil
}

// cachedStaticWritableDirs computes the static writable directory list
// (excluding CWD) once and caches the result. The CWD is excluded because
// it can change during execution; it is resolved fresh in IsWritablePath.
// The static dirs are: Go toolchain directories, the user config directory,
// /tmp, and /dev/shm.
var cachedStaticWritableDirs = sync.OnceValue(func() []string {
	var dirs []string
	seen := make(map[string]bool)

	addDir := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "." || dir == "/" {
			return
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	// Go toolchain writable directories.
	for _, dir := range resolveGoWritableDirs() {
		addDir(dir)
	}

	// User config directory (for memory and chat history persistence).
	if configDir := resolveConfigDir(); configDir != "" {
		addDir(configDir)
	}

	// /tmp and /dev/shm (tmpfs mounts inside container).
	addDir("/tmp")
	addDir("/dev/shm")

	return dirs
})

// cachedCanonicalStaticWritableDirs computes the canonicalized
// (EvalSymlinks) static writable directory list once and caches the
// result.
var cachedCanonicalStaticWritableDirs = sync.OnceValue(func() []string {
	var dirs []string
	for _, dir := range cachedStaticWritableDirs() {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dirs = append(dirs, resolved)
		} else {
			dirs = append(dirs, dir)
		}
	}
	return dirs
})

// usableDir returns the cleaned absolute form of dir when it names an existing
// directory, and the empty string otherwise. The sandbox write policy matches
// paths by prefix, so a value that no longer names a directory must not enter
// the set: a stale entry would widen the policy over a path that no longer
// exists. See TheoryOfWritableDirs.
func usableDir(dir string) string {
	dir = filepath.Clean(dir)
	if dir == "" || dir == "/" || dir == "." {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// goEnvValues holds the toolchain environment values the writable-set
// resolution consumes. One `go env -json` invocation returns all three, where
// three separate `go env <key>` invocations returned one each; the toolchain
// start is the expensive part, so the count of starts is what matters.
// See TheoryOfWritableDirs.
type goEnvValues struct {
	GOCACHE    string `json:"GOCACHE"`
	GOMODCACHE string `json:"GOMODCACHE"`
	GOPATH     string `json:"GOPATH"`
}

// resolveGoWritableDirs resolves directories that the Go toolchain needs
// write access to: GOCACHE (build cache), GOMODCACHE (downloaded modules),
// and GOPATH/pkg (package objects). Inside the container the directories come
// from the values the parent resolved before the mount restrictions, so a
// sandboxed start spawns no toolchain process; otherwise one `go env -json`
// invocation supplies every variable the environment leaves unset. These are
// resolved before entering the container namespace because `go env` may not
// function correctly after mount restrictions are applied. Directories that
// don't exist are skipped.
// See TheoryOfContainerIsolation in container_linux.go and
// TheoryOfWritableDirs.
func resolveGoWritableDirs() []string {
	var dirs []string
	seen := make(map[string]bool)

	addDir := func(dir string) {
		dir = usableDir(dir)
		if dir == "" {
			return
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	// The parent resolved the toolchain directories before the mount
	// restrictions and carried them in the environment, because `go env` may
	// not work inside the container. Reading them here keeps a sandboxed start
	// free of subprocesses, and makes this set identical to the mount layer's
	// writable set by construction. See TheoryOfWritableDirs.
	if pre := os.Getenv(goWritableDirsEnv); pre != "" {
		for _, dir := range filepath.SplitList(pre) {
			addDir(dir)
		}
		return dirs
	}

	gocache := os.Getenv("GOCACHE")
	gomodcache := os.Getenv("GOMODCACHE")
	gopath := os.Getenv("GOPATH")
	if gocache == "" || gomodcache == "" || gopath == "" {
		// One invocation resolves every unset variable, so a start that sets
		// none of them pays one toolchain start instead of three.
		values := readGoEnv()
		if gocache == "" {
			gocache = values.GOCACHE
		}
		if gomodcache == "" {
			gomodcache = values.GOMODCACHE
		}
		if gopath == "" {
			gopath = values.GOPATH
		}
	}

	addDir(gocache)
	addDir(gomodcache)
	for _, p := range filepath.SplitList(gopath) {
		addDir(filepath.Join(p, "pkg"))
	}

	return dirs
}

// readGoEnv resolves the toolchain environment values in one subprocess
// invocation. A missing `go` or a failed invocation yields zero values, and
// the caller then adds no toolchain directory. See TheoryOfWritableDirs.
func readGoEnv() goEnvValues {
	cmd := exec.Command("go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH")
	cmd.Env = os.Environ()
	output, err := cmd.Output()
	if err != nil {
		return goEnvValues{}
	}
	return parseGoEnvJSON(output)
}

// resolveConfigDir resolves the user config directory where the memory
// system (ai-memory.json) and chat history (ai-chat-history.json) persist
// data. Inside the container the directory comes from the value the parent
// resolved before the mount restrictions, because os.UserConfigDir may not
// function correctly after them. Returns an empty string if the directory
// does not exist or cannot be determined.
// See TheoryOfContainerIsolation in container_linux.go and
// TheoryOfWritableDirs.
func resolveConfigDir() string {
	if dir := usableDir(os.Getenv(configDirEnv)); dir != "" {
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return usableDir(dir)
}

// parseGoEnvJSON decodes one `go env -json` document. A malformed document
// yields zero values, so a toolchain whose output shape changed degrades to
// "no toolchain directories" instead of failing the caller.
// See TheoryOfWritableDirs.
func parseGoEnvJSON(output []byte) goEnvValues {
	var values goEnvValues
	if err := json.Unmarshal(output, &values); err != nil {
		return goEnvValues{}
	}
	return values
}
