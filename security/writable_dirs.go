package security

import (
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

// resolveGoWritableDirs resolves directories that the Go toolchain needs
// write access to: GOCACHE (build cache), GOMODCACHE (downloaded modules),
// and GOPATH/pkg (package objects). These are resolved before entering the
// container namespace because `go env` may not function correctly after
// mount restrictions are applied. Directories that don't exist are skipped.
// See TheoryOfContainerIsolation in container_linux.go.
func resolveGoWritableDirs() []string {
	var dirs []string
	seen := make(map[string]bool)

	addDir := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "/" || dir == "." {
			return
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	// GOCACHE: build cache directory.
	if dir := os.Getenv("GOCACHE"); dir != "" {
		addDir(dir)
	} else {
		addDir(goEnv("GOCACHE"))
	}

	// GOMODCACHE: module download cache.
	if dir := os.Getenv("GOMODCACHE"); dir != "" {
		addDir(dir)
	} else {
		addDir(goEnv("GOMODCACHE"))
	}

	// GOPATH/pkg: package object cache. GOPATH may contain multiple
	// paths separated by colons; each one's pkg subdirectory is added.
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = goEnv("GOPATH")
	}
	if gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			addDir(filepath.Join(p, "pkg"))
		}
	}

	return dirs
}

// resolveConfigDir resolves the user config directory where the memory
// system (ai-memory.json) and chat history (ai-chat-history.json) persist
// data. This is resolved before entering the container namespace because
// os.UserConfigDir may not function correctly after mount restrictions
// are applied. Returns an empty string if the directory does not exist
// or cannot be determined. See TheoryOfContainerIsolation in
// container_linux.go.
func resolveConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir = filepath.Clean(dir)
	if dir == "" || dir == "/" || dir == "." {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// goEnv runs `go env <key>` and returns the trimmed result. Returns an
// empty string if go is not available or the key is not set.
func goEnv(key string) string {
	cmd := exec.Command("go", "env", key)
	cmd.Env = os.Environ()
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
