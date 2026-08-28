package gotools

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/reusee/tai/logs"
)

const TheoryOfGitChangeOrdering = `
Root-module files — files belonging to the modules of the focus (root)
and context packages, typically a single project module — are ordered by
recent git activity to maximize LLM prefix cache reuse. A logical
package's change count is the sum over its files of the commits within the
most recent recentChangeCommitCount (50) commits that touched the file.
The counts are gathered with a single git log --max-count=N --name-only
invocation, preceded by git rev-parse --show-toplevel to resolve
repository-relative paths to absolute paths; counts are keyed by absolute
file path. During simplification, each root-module package accumulates the
counts of its files, and compareFilesForOutput sorts root-module files by
ascending change count, so the most-changed packages sit at the very end
of the root-module block. When a volatile file changes, the preceding
stable root-module and dependency content keeps its exact position,
preserving the cached prefix. Within a package, all files share the
package's change count, so the existing ordering keys (file path, etc.)
apply unchanged as tiebreakers.

The change-count key is compared after the root-package grouping, so
context files (non-root packages) always precede focus files: a change to
any focus file never shifts context or dependency content. Extending the
ordering from focus packages to the whole root module means context
packages in the same module are ordered the same way, so volatile context
content settles at the end of the context block instead of sitting in the
stable prefix region.

The window is a commit count rather than a fixed time range: a fixed time
window can be empty when no commits happen within it, producing all-zero
counts and losing the ordering signal. The most recent
recentChangeCommitCount commits always form a meaningful evaluation range
as long as the repository has any commits.

The git subprocesses run once per process from the load directory (or the
workspace root in workspace mode), matching the directory used to load
packages.

When the working directory is not inside a git repository, or git log
fails for any other reason, the feature degrades gracefully: change counts
are all zero and root-module files keep the deterministic package ordering.
`

// recentChangeCommitCount is the number of most recent commits over which
// git change counts are accumulated for root-module package ordering.
// Commits beyond this count do not influence the ordering: the heuristic
// assumes that packages touched by the most recent commits are more likely
// to change again in the near future. A commit-count window is used rather
// than a fixed time window so the evaluation range is always meaningful:
// a fixed time window can contain no commits, yielding all-zero counts
// and losing the ordering signal. See TheoryOfGitChangeOrdering.
const recentChangeCommitCount = 50

// GetGitChangeCounts returns a map from absolute file path to the number
// of commits within the most recent recentChangeCommitCount commits that
// touched the file. The map is computed once per process and is empty
// when the working directory is not inside a git repository. See
// TheoryOfGitChangeOrdering.
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
		counts, err := countGitChanges(dir, []string(envs), recentChangeCommitCount)
		if err != nil {
			// Not a git repository, or git log failed: degrade to zero
			// counts so root-module packages keep the deterministic
			// package ordering. See TheoryOfGitChangeOrdering.
			logger.Warn("git change counting disabled", "dir", dir, "error", err)
			return nil, nil
		}
		return counts, nil
	})
}

// countGitChanges returns a map from absolute file path to the number of
// commits within the most recent maxCommits commits that touched the
// file. Git reports paths relative to the repository root, so one git
// rev-parse --show-toplevel call resolves them to absolute paths, letting
// callers look up counts directly by file path. An error is returned when
// dir is not inside a git repository or the git commands fail; callers
// degrade to zero counts. See TheoryOfGitChangeOrdering.
func countGitChanges(dir string, envs []string, maxCommits int) (map[string]int, error) {
	rootCmd := exec.Command("git", "rev-parse", "--show-toplevel")
	rootCmd.Dir = dir
	rootCmd.Env = envs
	rootOut, err := rootCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	repoRoot := strings.TrimSpace(string(rootOut))

	cmd := exec.Command("git", "log", fmt.Sprintf("--max-count=%d", maxCommits), "--name-only", "--pretty=format:")
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
