//go:build linux

package security

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestResolveGoWritableDirsIncludesGOCACHE(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("GOCACHE", cacheDir)

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

func TestResolveGoWritableDirsSkipsNonExistent(t *testing.T) {
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

func TestResolveConfigDirSkipsNonExistent(t *testing.T) {
	// When HOME points to a non-existent directory, resolveConfigDir
	// should return an empty string rather than a stale path.
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/config/path")
	t.Setenv("HOME", "/nonexistent/home/path")
	dir := resolveConfigDir()
	if dir != "" {
		t.Fatalf("expected empty string for non-existent config dir, got %s", dir)
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
