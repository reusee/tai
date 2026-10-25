//go:build linux

package sandboxfs

import (
	"path/filepath"
	"testing"
)

// TestGoCacheDirsFollowsTheGoLayout checks that the default passthrough names
// the three Go cache variables of the environment, each resolved on its own:
// the caches are the largest read volume of a Go task and must not reach the
// store. See TheoryOfPassthrough.
func TestGoCacheDirsFollowsTheGoLayout(t *testing.T) {
	cache := t.TempDir()
	mods := t.TempDir()
	gopath := t.TempDir()
	t.Setenv("GOCACHE", cache)
	t.Setenv("GOMODCACHE", mods)
	t.Setenv("GOPATH", gopath)
	got := goCacheDirs()
	want := []string{cache, mods, filepath.Join(gopath, "pkg")}
	if len(got) != len(want) {
		t.Fatalf("goCacheDirs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("goCacheDirs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestGoCacheDirsSkipsTheOffCache checks the go command's "off" value: it
// names no directory, so the cache list never carries a relative path the
// mount would resolve against the working directory.
// See TheoryOfPassthrough.
func TestGoCacheDirsSkipsTheOffCache(t *testing.T) {
	t.Setenv("GOCACHE", "off")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	for _, p := range goCacheDirs() {
		if !filepath.IsAbs(p) {
			t.Fatalf("goCacheDirs() returned the relative path %q", p)
		}
	}
}

// TestDefaultPassthroughIncludesTheGoCaches checks that the default list
// carries the design's kernel filesystems, /tmp, and the Go caches together,
// so a caller that takes the defaults in one call gets both halves.
// See TheoryOfPassthrough.
func TestDefaultPassthroughIncludesTheGoCaches(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOCACHE", cache)
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOPATH", t.TempDir())
	paths := DefaultPassthrough()
	for _, want := range []string{"/dev", "/proc", "/sys", "/tmp", cache} {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("DefaultPassthrough() = %v, want %q among them", paths, want)
		}
	}
}

// TestDefaultPassthroughIncludesTheUserConfigDir checks the theory's fourth
// category: the user config directory bypasses the store like the caches do,
// so a tool that reads its own configuration pays no request per lookup.
// See TheoryOfPassthrough.
func TestDefaultPassthroughIncludesTheUserConfigDir(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	for _, p := range DefaultPassthrough() {
		if p == cfg {
			return
		}
	}
	t.Fatalf("DefaultPassthrough() does not carry %q", cfg)
}
