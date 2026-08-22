package gotools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
)

// initGitRepo initializes a git repository in dir with a fixed identity
// and returns a helper that runs git commands in the repository. The test
// is skipped when git is unavailable.
func initGitRepo(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("config", "user.name", "test")
	git("config", "user.email", "test@example.com")
	return git
}

func TestCountGitChanges(t *testing.T) {
	dir := t.TempDir()
	git := initGitRepo(t, dir)
	write := func(relPath, content string) {
		t.Helper()
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(relPath string) {
		t.Helper()
		git("add", relPath)
		git("commit", "-q", "-m", "update "+relPath)
	}

	// a/a.go is touched by three commits, b/b.go by one; the third
	// package c is unrelated. The counts must be per file within the
	// most recent recentChangeCommitCount commits, resolved against the
	// repository root.
	write("a/a.go", "package a\n")
	commit("a/a.go")
	write("b/b.go", "package b\n")
	commit("b/b.go")
	write("a/a.go", "package a\n\nfunc Foo() {}\n")
	commit("a/a.go")
	write("a/a.go", "package a\n\nfunc Foo() { println(1) }\n")
	commit("a/a.go")
	write("c/c.go", "package c\n")
	commit("c/c.go")

	counts, err := countGitChanges(dir, os.Environ(), recentChangeCommitCount)
	if err != nil {
		t.Fatal(err)
	}
	if got := counts[filepath.Join(dir, "a", "a.go")]; got != 3 {
		t.Fatalf("a/a.go change count = %d, want 3", got)
	}
	if got := counts[filepath.Join(dir, "b", "b.go")]; got != 1 {
		t.Fatalf("b/b.go change count = %d, want 1", got)
	}
	// Untracked files are absent from the map.
	write("d/d.go", "package d\n")
	if got := counts[filepath.Join(dir, "d", "d.go")]; got != 0 {
		t.Fatalf("untracked d/d.go change count = %d, want 0", got)
	}
}

func TestCountGitChangesNotARepo(t *testing.T) {
	// countGitChanges must report an error outside a git repository; the
	// provider degrades to zero counts instead, keeping focus package
	// ordering alphabetical. See TheoryOfGitChangeOrdering.
	dir := t.TempDir()
	if _, err := countGitChanges(dir, os.Environ(), recentChangeCommitCount); err == nil {
		t.Fatal("expected an error for a directory outside a git repository")
	}
}

func TestCountGitChangesUsesCommitCountWindow(t *testing.T) {
	// The evaluation range is the most recent commits, not a time window:
	// a fixed time window (e.g., the last three days) can contain no
	// commits, producing all-zero change counts and losing the ordering
	// signal. A commit-count window always yields a meaningful range as
	// long as the repository has any commits. A file touched only by
	// commits older than the window must not be counted; a file touched
	// within the window is counted. See TheoryOfGitChangeOrdering.
	dir := t.TempDir()
	git := initGitRepo(t, dir)

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(name string) {
		t.Helper()
		git("add", name)
		git("commit", "-q", "-m", "update "+name)
	}

	const window = 5
	// Three commits touch old.txt before the window; with a commit-count
	// evaluation range they must not be counted.
	for i := 0; i < 3; i++ {
		write("old.txt", fmt.Sprintf("old %d\n", i))
		commit("old.txt")
	}
	// Five commits touch new.txt within the window (one per commit).
	for i := 0; i < window; i++ {
		write("new.txt", fmt.Sprintf("new %d\n", i))
		commit("new.txt")
	}

	counts, err := countGitChanges(dir, os.Environ(), window)
	if err != nil {
		t.Fatal(err)
	}
	if got := counts[filepath.Join(dir, "old.txt")]; got != 0 {
		t.Fatalf("old.txt change count = %d, want 0 (outside the recent %d commits)", got, window)
	}
	if got := counts[filepath.Join(dir, "new.txt")]; got != window {
		t.Fatalf("new.txt change count = %d, want %d", got, window)
	}
}

func TestCompareFilesForOutputChangeCount(t *testing.T) {
	// Within the root-module block, files are ordered by ascending recent
	// git change count so the most-changed packages sit at the very end,
	// preserving the LLM prefix cache when volatile files change. The
	// change-count key applies to all root-module files — context
	// (non-root) packages as well as focus packages — but it is compared
	// after the root-package grouping, so context files always precede
	// focus files. Counts are zero outside a git repository, falling back
	// to the deterministic package ordering. See
	// TheoryOfGitChangeOrdering.
	makeFile := func(path string, changeCount int, isRoot bool) *File {
		return &File{
			Path:                    path,
			IsGoFile:                true,
			ModuleIsRoot:            true,
			PackageIsRoot:           isRoot,
			PackageDistanceFromRoot: 0,
			PackagePathDepth:        1,
			LogicalPkgPath:          "example.com/pkg",
			ChangeCount:             changeCount,
		}
	}

	files := []*File{
		makeFile("/repo/b.go", 1, true),
		makeFile("/repo/c.go", 3, true),
		makeFile("/repo/a.go", 2, true),
	}
	slices.SortStableFunc(files, compareFilesForOutput)
	want := []string{"/repo/b.go", "/repo/a.go", "/repo/c.go"}
	for i, f := range files {
		if f.Path != want[i] {
			t.Fatalf("position %d: got %s, want %s (order %v)", i, f.Path, want[i], pathsOf(files))
		}
	}

	// Equal change counts fall back to file-path ordering.
	files = []*File{
		makeFile("/repo/z.go", 1, true),
		makeFile("/repo/a.go", 1, true),
	}
	slices.SortStableFunc(files, compareFilesForOutput)
	if files[0].Path != "/repo/a.go" || files[1].Path != "/repo/z.go" {
		t.Fatalf("equal change counts should fall back to path ordering, got %v", pathsOf(files))
	}

	// Context files in the root module are ordered by change count too:
	// the key applies to all root-module files, so a context package
	// with more recent changes sorts later within the context block.
	// Alphabetically example.com/actx precedes example.com/bctx; the
	// change counts must override that.
	ctxFiles := []*File{
		makeFile("/repo/actx.go", 3, false),
		makeFile("/repo/bctx.go", 1, false),
	}
	slices.SortStableFunc(ctxFiles, compareFilesForOutput)
	if ctxFiles[0].Path != "/repo/bctx.go" || ctxFiles[1].Path != "/repo/actx.go" {
		t.Fatalf("context files should be ordered by change count, got %v", pathsOf(ctxFiles))
	}

	// Context files always precede focus files regardless of change
	// count: the root-package grouping is compared before the
	// change-count key.
	mixed := []*File{
		makeFile("/repo/focus.go", 0, true),
		makeFile("/repo/ctx.go", 100, false),
	}
	slices.SortStableFunc(mixed, compareFilesForOutput)
	if mixed[0].Path != "/repo/ctx.go" || mixed[1].Path != "/repo/focus.go" {
		t.Fatalf("context files must precede focus files, got %v", pathsOf(mixed))
	}
}

func pathsOf(files []*File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

func TestPartsOrdersFocusPackagesByGitChanges(t *testing.T) {
	// Focus packages with more recent git changes appear after focus
	// packages with fewer changes, so volatile content forms the tail of
	// the context and the stable prefix is preserved for LLM prefix
	// caching. With focus packages pinned at documentation level, the
	// packages appear as documentation blocks; the block carries the
	// logical package's change count, so the ordering key applies
	// unchanged. Alphabetically, example.com/changes/a precedes
	// example.com/changes/b; the git change counts must override that.
	// See TheoryOfGitChangeOrdering.
	root := t.TempDir()
	t.Setenv("GOWORK", "")
	git := initGitRepo(t, root)
	write := func(relPath, content string) {
		t.Helper()
		fullPath := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(relPath string) {
		t.Helper()
		git("add", relPath)
		git("commit", "-q", "-m", "update "+relPath)
	}

	write("go.mod", "module example.com/changes\n\ngo 1.21\n")
	commit("go.mod")

	// Package a accumulates three commits while package b gets one.
	write("a/a.go", "package a\n")
	commit("a/a.go")
	write("a/a.go", "package a\n\nfunc Foo() {}\n")
	commit("a/a.go")
	write("a/a.go", "package a\n\nfunc Foo() { println(1) }\n")
	commit("a/a.go")
	write("b/b.go", "package b\n")
	commit("b/b.go")

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)
	scope.Fork(
		func() LoadDir { return LoadDir(root) },
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatalf("Parts failed: %v", err)
		}
		posA, posB := -1, -1
		for i, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "begin of focus package example.com/changes/a") {
				posA = i
			}
			if strings.Contains(s, "begin of focus package example.com/changes/b") {
				posB = i
			}
		}
		if posA == -1 || posB == -1 {
			t.Fatalf("focus package documentation for a or b not found in parts (posA=%d posB=%d)", posA, posB)
		}
		if posA <= posB {
			t.Fatalf("a (3 commits) must appear after b (1 commit), got posA=%d posB=%d", posA, posB)
		}
	})
}

func TestPartsOrdersContextPackagesByGitChanges(t *testing.T) {
	// Context files in the root module are ordered by recent git activity
	// just like focus packages: the change-count key applies to all
	// root-module files, so volatile context content settles at the end of
	// the context block instead of sitting in the stable prefix region.
	// Alphabetically, example.com/mod/actx precedes example.com/mod/bctx;
	// the git change counts must override that. The focus package is
	// pinned at documentation level; its block carries the package's
	// change count and interleaves with the context files under the same
	// ordering, so only the context ordering (bctx before actx) and the
	// block's presence are asserted. See TheoryOfGitChangeOrdering.
	root := t.TempDir()
	t.Setenv("GOWORK", "")
	git := initGitRepo(t, root)
	write := func(relPath, content string) {
		t.Helper()
		fullPath := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(relPath string) {
		t.Helper()
		git("add", relPath)
		git("commit", "-q", "-m", "update "+relPath)
	}

	write("go.mod", "module example.com/mod\n\ngo 1.21\n")
	commit("go.mod")

	// Package actx accumulates three commits while bctx gets one.
	write("actx/actx.go", "package actx\n")
	commit("actx/actx.go")
	write("actx/actx.go", "package actx\n\nfunc Foo() {}\n")
	commit("actx/actx.go")
	write("actx/actx.go", "package actx\n\nfunc Foo() { println(1) }\n")
	commit("actx/actx.go")
	write("bctx/bctx.go", "package bctx\n")
	commit("bctx/bctx.go")

	// The focus package lives in the same module; actx and bctx are
	// loaded as context packages and are not root packages.
	write("focus/focus.go", "package focus\n")
	commit("focus/focus.go")

	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)
	scope.Fork(
		func() LoadDir { return LoadDir(root) },
		func() LoadPatterns { return LoadPatterns{"./focus/..."} },
		func() ContextPatterns { return ContextPatterns{"./actx", "./bctx"} },
	).Call(func(
		provider CodeProvider,
		countTokens generators.BPETokenCounter,
	) {
		parts, err := provider.Parts(1<<20, countTokens, nil)
		if err != nil {
			t.Fatalf("Parts failed: %v", err)
		}
		posActx, posBctx, posFocus := -1, -1, -1
		for i, part := range parts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			s := string(text)
			if strings.Contains(s, "begin of context file "+filepath.Join(root, "actx", "actx.go")) {
				posActx = i
			}
			if strings.Contains(s, "begin of context file "+filepath.Join(root, "bctx", "bctx.go")) {
				posBctx = i
			}
			if strings.Contains(s, "begin of focus package example.com/mod/focus") {
				posFocus = i
			}
		}
		if posActx == -1 || posBctx == -1 || posFocus == -1 {
			t.Fatalf("context files or focus package documentation not found in parts (posActx=%d posBctx=%d posFocus=%d)", posActx, posBctx, posFocus)
		}
		if posBctx >= posActx {
			t.Fatalf("bctx (1 commit) must appear before actx (3 commits), got posBctx=%d posActx=%d", posBctx, posActx)
		}
	})
}
