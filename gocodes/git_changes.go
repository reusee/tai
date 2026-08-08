package gocodes

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/reusee/tai/logs"
)

const TheoryOfGitChangeOrdering = `
Focus packages are ordered by recent git activity to maximize LLM prefix
cache reuse. A logical focus package's change count is the sum over its
files of the commits within recentChangeWindow (three days) that touched
the file. The counts are gathered with a single git log --name-only
invocation, preceded by git rev-parse --show-toplevel to resolve
repository-relative paths to absolute paths; counts are keyed by absolute
file path. During simplification, each focus package accumulates the
counts of its files, and compareFilesForOutput sorts focus files by
ascending change count, so the most-changed packages sit at the very end
of the focus block. When a volatile focus file changes, the preceding
stable focus and context content keeps its exact position, preserving the
cached prefix. Within a package, all files share the package's change
count, so the existing ordering keys (file path, etc.) apply unchanged as
tiebreakers.

The ordering applies to focus (root) packages only: context files form
the stable prefix region and keep their deterministic ordering, so focus
file changes cannot shift context content. The git subprocesses run once
per process from the load directory (or the workspace root in workspace
mode), matching the directory used to load packages.

When the working directory is not inside a git repository, or git log
fails for any other reason, the feature degrades gracefully: change counts
are all zero and focus packages keep the alphabetical package-path
ordering.
`

// recentChangeWindow is the time window over which git change counts are
// accumulated for focus package ordering. Commits older than this window
// do not influence the ordering: the heuristic assumes that packages
// touched recently are more likely to change again in the near future.
// See TheoryOfGitChangeOrdering.
const recentChangeWindow = 3 * 24 * time.Hour

// GetGitChangeCounts returns a map from absolute file path to the number
// of commits within recentChangeWindow that touched the file. The map is
// computed once per process and is empty when the working directory is
// not inside a git repository. See TheoryOfGitChangeOrdering.
type GetGitChangeCounts func() (map[string]int, error)

func (Module) GitChangeCounts(
	logger logs.Logger,
	loadDir LoadDir,
	workspace Workspace,
	envs Envs,
) GetGitChangeCounts {
	return sync.OnceValues(func() (map[string]int, error) {
		dir := string(loadDir)
		if workspace != "" {
			dir = string(workspace)
		}
		counts, err := countGitChanges(dir, []string(envs))
		if err != nil {
			// Not a git repository, or git log failed: degrade to zero
			// counts so focus packages keep the alphabetical package-path
			// ordering. See TheoryOfGitChangeOrdering.
			logger.Warn("git change counting disabled", "dir", dir, "error", err)
			return nil, nil
		}
		return counts, nil
	})
}

// countGitChanges returns a map from absolute file path to the number of
// commits within recentChangeWindow that touched the file. Git reports
// paths relative to the repository root, so one git rev-parse
// --show-toplevel call resolves them to absolute paths, letting callers
// look up counts directly by file path. An error is returned when dir is
// not inside a git repository or the git commands fail; callers degrade
// to zero counts. See TheoryOfGitChangeOrdering.
func countGitChanges(dir string, envs []string) (map[string]int, error) {
	rootCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	rootCmd.Dir = dir
	rootCmd.Env = envs
	rootOut, err := rootCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	repoRoot := strings.TrimSpace(string(rootOut))

	since := fmt.Sprintf("--since=%d days ago", int(recentChangeWindow/(24*time.Hour)))
	cmd := exec.Command("git", "log", since, "--name-only", "--pretty=format:")
	cmd.Dir = dir
	cmd.Env = envs
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	counts := make(map[string]int)
	for _, line := range strings.Split(string(out), "\n") {
		relPath := strings.TrimSpace(line)
		if relPath == "" {
			continue
		}
		absPath := filepath.Clean(filepath.Join(repoRoot, filepath.FromSlash(relPath)))
		counts[absPath]++
	}
	return counts, nil
}
