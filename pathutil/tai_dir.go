package pathutil

import (
	"os"
	"path/filepath"
)

// TheoryOfTaiDir explains the tai dir: the single directory that holds a
// working tree's tai state.
const TheoryOfTaiDir = `
The tai dir is the one directory that holds the tai state of a tree: the
.tai directory with the self-prompt and the files stored beside it. It
resolves upward from the working directory, so every invocation inside a
tree reads and writes the same state and no invocation creates a private
copy in its own subdirectory — the rule go.mod and .git already follow.

The tai dir is a distinct type, pathutil.TaiDir, not a bare string. The
command resolves it once through ResolveTaiDir, records the value for the
sandbox, and pins it in the dscope scope; every package that persists state
takes the resolved value from the scope. One resolution keeps the sandbox
rule and every state file on the same directory, and a new state file needs
no new walk. A package must not resolve a tai dir of its own: two
resolutions can disagree, and the disagreement surfaces as state that one
invocation writes and another never reads. A scope-level override wins the
resolution, which is how the command pins its own value and how a test
steers the state into a temporary directory.

The type owns the path arithmetic. StateDir and StatePath build the .tai
directory and a named state file from it, so a caller never joins paths by
hand and the state-directory name lives in one constant. The tai dir itself
is never a state file's parent: the .tai directory is, and a state file
written beside the tai dir would escape the directory the sandbox allows.

The nearest go.mod directory wins: the module is the unit whose users share
one tai state. Outside a module the git worktree root wins — the directory
holding the .git entry, which for a linked worktree is the worktree's top
level because a worktree keeps its .git entry as a file. A directory inside
both keeps the module: a repository that holds several modules gives each
module its own state, while a repository without modules keeps one state at
its root.

A directory inside neither is its own tai dir, so a scratch tree keeps a
private .tai and works without version control.

The tai dir is resolved from the working directory, never from the target of
a write. It may sit above the working directory, where the sandbox is
read-only by default, so the container layer is told the state directory
before the sandbox starts. See security.TheoryOfWritableDirs.
`

// StateDirName names the directory a tai dir keeps its state files in.
const StateDirName = ".tai"

// FindGitRoot returns the absolute path of the nearest directory at or above
// dir holding a .git entry, and whether one exists. A .git file counts: a
// linked worktree and a submodule keep their .git entry as a file, and the
// directory holding it is the worktree's top level. See TheoryOfTaiDir.
func FindGitRoot(dir string) (root string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		dir = filepath.Clean(dir)
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// FindTaiDir returns the tai dir of dir: the nearest go.mod directory, else
// the git worktree root, else dir itself, always as an absolute path. See
// TheoryOfTaiDir.
func FindTaiDir(dir string) string {
	if root, ok := FindGoModuleRoot(dir); ok {
		return root
	}
	if root, ok := FindGitRoot(dir); ok {
		return root
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}
