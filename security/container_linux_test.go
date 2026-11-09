//go:build linux

package security

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

// TestResolveGoWritableDirsIncludesGOCACHE checks the fallback resolution:
// with no pre-resolved value in the environment, an explicit GOCACHE enters
// the set. A test process running inside the sandbox inherits the parent's
// pre-resolved value, so the test clears it; the pre-resolved precedence has
// its own test. Every variable is set so the resolution spawns no toolchain.
// See TheoryOfWritableDirs.
func TestResolveGoWritableDirsIncludesGOCACHE(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GOCACHE", cacheDir)
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv(goWritableDirsEnv, "")

	dirs := resolveGoWritableDirs()

	found := false
	for _, d := range dirs {
		if d == filepath.Clean(cacheDir) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected GOCACHE dir %s in result: %v", filepath.Clean(cacheDir), dirs)
	}
}

func TestResolveGoWritableDirsAllExist(t *testing.T) {
	dirs := resolveGoWritableDirs()
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatalf("non-existent directory in result: %s", d)
		}
		if !info.IsDir() {
			t.Fatalf("non-directory in result: %s", d)
		}
	}
}

// TestResolveGoWritableDirsPrefersPreResolved checks that a sandboxed start
// reads the toolchain directories the parent resolved before the mount
// restrictions, instead of spawning the Go toolchain inside the sandbox. The
// empty PATH makes a spawn impossible, so the pre-resolved value is the only
// way the directory can enter the set. See TheoryOfWritableDirs.
func TestResolveGoWritableDirsPrefersPreResolved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(goWritableDirsEnv, dir)
	t.Setenv("GOCACHE", "/nonexistent/cache")
	t.Setenv("GOMODCACHE", "/nonexistent/mod")
	t.Setenv("GOPATH", "/nonexistent/gopath")
	t.Setenv("PATH", t.TempDir())

	dirs := resolveGoWritableDirs()

	if !slices.Contains(dirs, filepath.Clean(dir)) {
		t.Fatalf("pre-resolved directory %s missing from %v", dir, dirs)
	}
}

// TestResolveGoWritableDirsSkipsNonExistent checks that the fallback
// resolution drops a configured value that names no existing directory: the
// set feeds a prefix-matched write policy, so a stale entry would widen it.
// The pre-resolved value is cleared for the same reason as in the GOCACHE
// test. See TheoryOfWritableDirs.
func TestResolveGoWritableDirsSkipsNonExistent(t *testing.T) {
	t.Setenv(goWritableDirsEnv, "")
	t.Setenv("GOCACHE", "/nonexistent/path/should/be/skipped")
	t.Setenv("GOMODCACHE", "/nonexistent/mod/path")
	t.Setenv("GOPATH", "/nonexistent/gopath")

	dirs := resolveGoWritableDirs()
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("non-existent directory should not be in result: %s", d)
		}
	}
}

func TestResolveConfigDir(t *testing.T) {
	dir := resolveConfigDir()
	// resolveConfigDir should return the same path as os.UserConfigDir
	// when the directory exists, or empty string when it doesn't.
	expectedDir, err := os.UserConfigDir()
	if err != nil {
		if dir != "" {
			t.Fatalf("expected empty dir when UserConfigDir fails, got %s", dir)
		}
		return
	}
	expectedDir = filepath.Clean(expectedDir)
	if dir != expectedDir {
		t.Fatalf("expected %s, got %s", expectedDir, dir)
	}
	if dir != "" {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("config dir does not exist: %s", dir)
		}
		if !info.IsDir() {
			t.Fatalf("config dir is not a directory: %s", dir)
		}
	}
}

// TestResolveConfigDirSkipsNonExistent checks the fallback resolution: when
// the config location points at a non-existent directory, resolveConfigDir
// returns an empty string rather than a stale path. A test process running
// inside the sandbox inherits the parent's pre-resolved value, so the test
// clears it. See TheoryOfWritableDirs.
func TestResolveConfigDirSkipsNonExistent(t *testing.T) {
	t.Setenv(configDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/config/path")
	t.Setenv("HOME", "/nonexistent/home/path")
	dir := resolveConfigDir()
	if dir != "" {
		t.Fatalf("expected empty string for non-existent config dir, got %s", dir)
	}
}

// TestResolveConfigDirPrefersPreResolved checks that the sandboxed start reads
// the config directory the parent resolved, even when os.UserConfigDir would
// name a different one. See TheoryOfWritableDirs.
func TestResolveConfigDirPrefersPreResolved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/config/path")
	t.Setenv("HOME", "/nonexistent/home/path")

	if got := resolveConfigDir(); got != filepath.Clean(dir) {
		t.Fatalf("resolveConfigDir = %q, want %q", got, filepath.Clean(dir))
	}
}

func TestParseMountPoints(t *testing.T) {
	mounts, err := parseMountPoints()
	if err != nil {
		t.Fatalf("parseMountPoints failed: %v", err)
	}
	if len(mounts) == 0 {
		t.Fatal("expected at least one mount point")
	}
	foundRoot := slices.Contains(mounts, "/")
	if !foundRoot {
		t.Fatal("expected / in mount points")
	}
}

func TestSetNoNewPrivsDoesNotPanic(t *testing.T) {
	// setNoNewPrivs should not panic or cause any error.
	// It is a best-effort operation that may silently fail.
	setNoNewPrivs()
}

func TestTmpfsMountData(t *testing.T) {
	// No size option when env not set.
	if got := tmpfsMountData("CAI_TEST_TMPFS_UNSET"); got != "" {
		t.Fatalf("expected empty mount data, got %q", got)
	}
	// Size option when env set.
	t.Setenv("CAI_TEST_TMPFS_SET", "1g")
	if got := tmpfsMountData("CAI_TEST_TMPFS_SET"); got != "size=1g" {
		t.Fatalf("expected size=1g, got %q", got)
	}
	// Empty env falls back to no size option.
	t.Setenv("CAI_TEST_TMPFS_EMPTY", "")
	if got := tmpfsMountData("CAI_TEST_TMPFS_EMPTY"); got != "" {
		t.Fatalf("expected empty mount data for empty env, got %q", got)
	}
}

func TestMountWritableBindUsesBindRemount(t *testing.T) {
	var flags []uintptr
	prev := mountSyscall
	mountSyscall = func(_, _, _ string, f uintptr, _ string) error {
		flags = append(flags, f)
		return nil
	}
	defer func() { mountSyscall = prev }()
	if err := mountWritableBind("/dir"); err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("mount calls %d, want 2", len(flags))
	}
	if flags[0] != syscall.MS_BIND {
		t.Fatalf("first mount flags %#x, want MS_BIND", flags[0])
	}
	if flags[1] != syscall.MS_REMOUNT|syscall.MS_BIND {
		t.Fatalf("remount flags %#x, want MS_REMOUNT|MS_BIND", flags[1])
	}
}

// TestSetupContainerFilesystemTmpfsKeepsWritableDirs checks that a fresh
// tmpfs never hides a writable directory: replacing /tmp while the working
// directory lives under it would make every relative path resolve elsewhere
// and the following writable bind fail. See TheoryOfContainerIsolation.
func TestSetupContainerFilesystemTmpfsKeepsWritableDirs(t *testing.T) {
	t.Setenv(goWritableDirsEnv, "")
	t.Setenv(configDirEnv, "")
	const cwd = "/tmp/tai-work-dir"

	var tmpfs []string
	prev := mountSyscall
	mountSyscall = func(_, target, fstype string, _ uintptr, _ string) error {
		if fstype == "tmpfs" {
			tmpfs = append(tmpfs, target)
		}
		return nil
	}
	defer func() { mountSyscall = prev }()

	if err := setupContainerFilesystem(cwd); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tmpfs, "/tmp") {
		t.Fatalf("/tmp became tmpfs although the working directory %s lies under it", cwd)
	}
	if !slices.Contains(tmpfs, "/dev/shm") {
		t.Fatalf("/dev/shm should keep its fresh tmpfs, got %v", tmpfs)
	}
}

// TestSetupContainerFilesystemHardensBeforeTmpfs checks that the pre-existing
// submounts are hardened before the fresh tmpfs mounts: the hardening
// enumerates the namespace's mounts, so a tmpfs mounted first would be seen as
// a pre-existing submount and lose the writability it was created for.
// See TheoryOfContainerIsolation.
func TestSetupContainerFilesystemHardensBeforeTmpfs(t *testing.T) {
	t.Setenv(goWritableDirsEnv, "")
	t.Setenv(configDirEnv, "")
	const cwd = "/home/tai-work-dir"

	var calls int
	var harden, tmpfs []int
	prev := mountSyscall
	mountSyscall = func(_, target, fstype string, f uintptr, _ string) error {
		if fstype == "tmpfs" {
			tmpfs = append(tmpfs, calls)
		}
		if target != "/" && f == syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY {
			harden = append(harden, calls)
		}
		calls++
		return nil
	}
	defer func() { mountSyscall = prev }()

	if err := setupContainerFilesystem(cwd); err != nil {
		t.Fatal(err)
	}
	if len(tmpfs) != len(tmpfsMounts) {
		t.Fatalf("tmpfs mounts %d, want %d", len(tmpfs), len(tmpfsMounts))
	}
	if len(harden) == 0 {
		t.Skip("the host exposes no submount outside the kernel filesystems and the writable set")
	}
	if last := harden[len(harden)-1]; last > tmpfs[0] {
		t.Fatalf("submount hardening at call %d runs after the first tmpfs mount at call %d", last, tmpfs[0])
	}
}

// TestSetupContainerFilesystemFailureKeepsRootWritable checks that a writable
// mount failure aborts the setup before the root turns read-only, so the
// warning path really leaves the process on its inherited policy instead of a
// read-only working directory. See TheoryOfContainerIsolation.
func TestSetupContainerFilesystemFailureKeepsRootWritable(t *testing.T) {
	t.Setenv(goWritableDirsEnv, "")
	t.Setenv(configDirEnv, "")
	cwd := t.TempDir()

	rootReadOnly := false
	prev := mountSyscall
	mountSyscall = func(_, target, _ string, f uintptr, _ string) error {
		if target == "/" && f&syscall.MS_RDONLY != 0 {
			rootReadOnly = true
		}
		if target == cwd {
			return syscall.EPERM
		}
		return nil
	}
	defer func() { mountSyscall = prev }()

	if err := setupContainerFilesystem(cwd); err == nil {
		t.Fatal("want an error when the working directory cannot be made writable")
	}
	if rootReadOnly {
		t.Fatal("the root turned read-only although the writable mount failed")
	}
}

// TestSetupContainerFilesystemWritableBeforeRootReadOnly checks the order that
// keeps a failed setup from leaving the working directory read-only: the
// writable mounts exist before the root turns read-only, so the best-effort
// fallback keeps the inherited policy instead of a read-only working
// directory. See TheoryOfContainerIsolation.
func TestSetupContainerFilesystemWritableBeforeRootReadOnly(t *testing.T) {
	t.Setenv(goWritableDirsEnv, "")
	t.Setenv(configDirEnv, "")
	cwd := t.TempDir()

	var targets []string
	var flags []uintptr
	prev := mountSyscall
	mountSyscall = func(_, target, _ string, f uintptr, _ string) error {
		targets = append(targets, target)
		flags = append(flags, f)
		return nil
	}
	defer func() { mountSyscall = prev }()

	if err := setupContainerFilesystem(cwd); err != nil {
		t.Fatal(err)
	}

	writableBind, rootReadOnly := -1, -1
	for i, target := range targets {
		if target == cwd && flags[i] == syscall.MS_BIND {
			writableBind = i
		}
		if target == "/" && flags[i]&syscall.MS_RDONLY != 0 {
			rootReadOnly = i
		}
	}
	if writableBind < 0 {
		t.Fatalf("the working directory was never bound writable: %v", targets)
	}
	if rootReadOnly < 0 {
		t.Fatalf("the root was never made read-only: %v", targets)
	}
	if writableBind > rootReadOnly {
		t.Fatalf("the working directory bound writable at call %d, after the root turned read-only at call %d", writableBind, rootReadOnly)
	}
}

// TestParseGoEnvJSON checks the one-invocation decoder: the three values
// decode from one document, and a malformed document yields zero values so a
// changed output shape degrades to "no toolchain directories" instead of
// failing the caller. See TheoryOfWritableDirs.
func TestParseGoEnvJSON(t *testing.T) {
	values := parseGoEnvJSON([]byte(`{"GOCACHE":"/c","GOMODCACHE":"/m","GOPATH":"/g"}`))
	if values.GOCACHE != "/c" || values.GOMODCACHE != "/m" || values.GOPATH != "/g" {
		t.Fatalf("decoded %+v", values)
	}
	if values := parseGoEnvJSON([]byte(`not json`)); values != (goEnvValues{}) {
		t.Fatalf("malformed document decoded to %+v", values)
	}
}
