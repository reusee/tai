package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindGitRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindGitRoot(sub); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}
	if got, ok := FindGitRoot(root); !ok || got != root {
		t.Fatalf("expected %q, got %q, ok %v", root, got, ok)
	}

	// A linked worktree keeps its .git entry as a file, and the directory
	// holding that file is the worktree's top level.
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := FindGitRoot(worktree); !ok || got != worktree {
		t.Fatalf("expected %q, got %q, ok %v", worktree, got, ok)
	}

	// No .git anywhere above: not found.
	if got, ok := FindGitRoot(t.TempDir()); ok || got != "" {
		t.Fatalf("expected not found, got %q, ok %v", got, ok)
	}
}

func TestFindTaiDir(t *testing.T) {
	// A directory inside neither a module nor a repository is its own tai
	// dir: no marker exists above it, so the working directory itself is
	// the boundary and a scratch tree keeps a private .tai.
	scratch := t.TempDir()
	scratchSub := filepath.Join(scratch, "a")
	if err := os.MkdirAll(scratchSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindTaiDir(scratchSub); got != scratchSub {
		t.Fatalf("expected %q, got %q", scratchSub, got)
	}
	if got := FindTaiDir(scratch); got != scratch {
		t.Fatalf("expected %q, got %q", scratch, got)
	}

	// A repository without modules answers with its root.
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoSub := filepath.Join(repo, "deep", "dir")
	if err := os.MkdirAll(repoSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindTaiDir(repoSub); got != repo {
		t.Fatalf("expected %q, got %q", repo, got)
	}

	// A module inside the repository answers with the module root: each
	// module owns its state.
	module := filepath.Join(repo, "mod")
	if err := os.MkdirAll(module, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	moduleSub := filepath.Join(module, "a", "b")
	if err := os.MkdirAll(moduleSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindTaiDir(moduleSub); got != module {
		t.Fatalf("expected %q, got %q", module, got)
	}
	if got := FindTaiDir(module); got != module {
		t.Fatalf("expected %q, got %q", module, got)
	}
	// The repository root itself stays its own tai dir: no module encloses
	// it, and the module below it does not reach upward.
	if got := FindTaiDir(repo); got != repo {
		t.Fatalf("expected %q, got %q", repo, got)
	}
}

func TestStateDir(t *testing.T) {
	if got := StateDir("/proj"); got != filepath.Join("/proj", ".tai") {
		t.Fatalf("got %q", got)
	}
}
