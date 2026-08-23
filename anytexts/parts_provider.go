package anytexts

import (
	"cmp"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfReadOnlySymlinks = `
Symbolic links are followed so that content from other directories or files can
be included via symlinks. Cycle detection uses two complementary mechanisms:
1. Ancestor check: a symlink whose resolved target is an ancestor of the current
   path would create an infinite loop and is skipped.
2. Visited set: a map of resolved real paths records every symlink target that
   has been followed. If a symlink resolves to a path already in the set, it is
   skipped to break cycles that do not involve an ancestor relationship (e.g.,
   mutual symlinks between sibling directories).
Broken symlinks whose targets cannot be resolved are silently skipped rather than
aborting the entire traversal.

Directly-specified focus files that resolve outside writable directories are
marked read-only at collection time; see TheoryOfFocusFileDirectoryCheck.
The read-only annotation applies to both directly-specified focus files
and files discovered during directory traversal, ensuring the model is
consistently informed that these files cannot be modified.

The check resolves symlinks in the path via isOutsideWritableDirs, which
delegates to pathutil.IsOutsideWritableDirs, to correctly handle symlinks
that point outside writable directories. A symlink within a writable
directory whose target is outside is marked read-only, because writing to
it would write outside the writable directories.
`

const TheoryOfFocusFileDirectoryCheck = `
Focus files (files directly specified via patterns) that resolve to a
location outside all writable directories are marked as read-only at
collection time rather than rejected. The writable directories are
determined by the security package's container filesystem policy: the
current working directory, Go toolchain directories (GOCACHE, GOMODCACHE,
GOPATH/pkg), the user config directory, /tmp, and /dev/shm. This ensures
the check is consistent with the security package's container isolation —
no more and no less restrictive. A focus file outside writable directories
can still provide useful reference context even though it cannot be
modified; marking it as read-only informs the model that change blocks
must not target it, while still allowing its content to inform changes
to writable project files.

This check applies to directly-matched patterns (directMatch=true), not to
files discovered during directory traversal. Files discovered via symlinks
during traversal are also marked read-only (see TheoryOfReadOnlySymlinks)
via the same mechanism.

The check resolves symlinks in the path via pathutil.IsOutsideWritableDirs
to correctly handle symlinks that point outside writable directories. A
symlink within a writable directory whose target is outside is marked
read-only, because writing to it would write outside the writable
directories.
`

const TheoryOfFileOrdering = `
Files are sorted by path as the primary key to ensure a fully deterministic
order that is independent of modification times. Using modification time as
the primary sort key would cause reordering whenever timestamps change
(e.g., after a git checkout or touch), destroying the LLM prefix cache.
Path-based ordering guarantees that unchanged files always appear in the
same position, maximizing cache reuse across requests. Modification time
is retained only as a final tiebreaker for the hypothetical case where two
files share the same path (impossible in practice).
`

const TheoryOfWorkingDirectoryHint = `
The working directory hint appends the absolute path of the process working
directory after all file contents provided by PartsProvider, together with a
directive to construct change block file-path attributes as absolute paths
within it. The apply layer resolves such paths relative to the working
directory, so the hint gives the model the base path it needs instead of
guessing from file context that may live in a different directory.

The working directory is dynamic content that changes per invocation, so the
hint is positioned at the very end of the provider parts: the file contents
stay byte-identical in the LLM prefix cache across runs in different
directories, and only the trailing hint changes.
`

const TheoryOfBinaryFileMarkers = `
Binary files included in the model context must be wrapped with begin/end markers
matching the text file format, so the model can identify the attachment boundary.
The marker includes the MIME type to help the model understand the content type.
Without end markers, the model cannot determine where the binary attachment ends
and the next file's content begins, especially when multiple binary files are
included consecutively.
`

const TheoryOfPatternMatching = `
All file pattern matching — glob expansion and path matching — is unified on the
doublestar library (github.com/bmatcuk/doublestar/v4). Glob expansion uses
doublestar.FilepathGlob for native ** (globstar) support for recursive directory
traversal. Pattern matching uses doublestar.PathMatch, also supporting **
patterns. Non-glob exclusion patterns (e.g., "pkg") retain directory-prefix
matching semantics alongside the doublestar path match, so "pkg" excludes both
"pkg" itself and everything under "pkg/". Slash-less exclusion patterns (e.g.,
"*.md" or "README.md") additionally match the path's basename at any depth,
following gitignore-style semantics, so files in subdirectories or sibling
workspace modules (paths containing "..") are still excluded. This ensures
consistent ** semantics across all file matching contexts: IterFiles glob
expansion, isExcludedPath pattern matching, request-context glob tags, and
gotools exclusion/embed-requested checks.

Hidden files (those whose basename starts with ".") are skipped during
directory traversal to avoid including unintended dotfiles (e.g., .git,
.env). However, when a user explicitly specifies a hidden file via a
pattern (e.g., -file .env), the file is included because explicit user
intent overrides the default skip behavior. The distinction is tracked by
marking pattern-matched paths as direct matches, while paths discovered
during directory traversal are not.
`

type PartsProvider struct {
	FileNameOK       dscope.Inject[FileNameOK]
	NameMatch        dscope.Inject[NameMatch]
	Logger           dscope.Inject[logs.Logger]
	Debug            dscope.Inject[Debug]
	IncludeMimeTypes dscope.Inject[IncludeMimeTypes]
}

var _ codetypes.PartsProvider = PartsProvider{}

type FileInfo struct {
	Path     string
	Content  []byte
	IsText   bool
	MimeType string
	ModTime  time.Time
	ReadOnly bool
}

func (c PartsProvider) IterFiles(patterns []string) iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {

		if len(patterns) == 0 {
			patterns = []string{"."}
		}

		// Collect candidate files with modification times
		type candidate struct {
			path     string
			modTime  time.Time
			readOnly bool
		}
		var candidates []candidate

		// queueItem tracks whether a path was directly matched via a
		// user-supplied pattern (directMatch=true) or discovered during
		// directory traversal (directMatch=false). Directly matched
		// paths are not subject to hidden-file filtering, so users can
		// explicitly include hidden files (e.g., .env, .gitignore) via
		// -file without having them silently skipped.
		type queueItem struct {
			path        string
			directMatch bool
		}
		var queue []queueItem

		for _, pattern := range patterns {
			files, err := doublestar.FilepathGlob(pattern)
			if err != nil {
				// use as-is
				queue = append(queue, queueItem{path: pattern, directMatch: true})
			} else {
				slices.SortStableFunc(files, cmp.Compare[string])
				for _, f := range files {
					queue = append(queue, queueItem{path: f, directMatch: true})
				}
			}
		}

		// Track symlink paths that point to directories outside the current
		// directory. Files discovered under these paths are marked as read-only
		// because they reside outside the project tree.
		externalSymlinkDirs := make(map[string]bool)

		// Track resolved real paths of symlink targets that have been followed.
		// This breaks cycles that the ancestor check alone cannot detect, such
		// as mutual symlinks between sibling directories.
		visitedSymlinks := make(map[string]bool)

		for len(queue) > 0 {
			item := queue[0]
			queue = queue[1:]
			path := item.path

			baseName := filepath.Base(path)
			// ignore hidden files, but not when directly matched via
			// user-supplied patterns. This allows users to explicitly
			// include hidden files (e.g., .env, .gitignore) via -file.
			if !item.directMatch && baseName != "." && strings.HasPrefix(baseName, ".") {
				continue
			}
			// ignore _ files
			if strings.HasPrefix(baseName, "_") {
				continue
			}

			// Use Lstat to detect symlinks without following them, so we can
			// guard against cycles when following symbolic links.
			info, err := os.Lstat(path)
			if err != nil {
				yield(FileInfo{}, err)
				return
			}

			var readOnly bool

			// Check if this path is under a directory that was introduced via
			// a symlink to an external location. Files under such directories
			// inherit the read-only status.
			if isUnderExternalDir(path, externalSymlinkDirs) {
				readOnly = true
			}

			// Follow symbolic links while detecting cycles: a symlink whose
			// resolved target is an ancestor of the current path would create
			// an infinite loop and is skipped. Additionally, a visited set of
			// resolved real paths breaks cycles that do not involve an ancestor
			// relationship. Broken symlinks are silently skipped rather than
			// aborting the traversal.
			if info.Mode()&os.ModeSymlink != 0 {
				realPath, err := filepath.EvalSymlinks(path)
				if err != nil {
					// Broken or unresolved symlink; skip silently.
					continue
				}
				if isAncestor(realPath, path) {
					// Symlink cycle detected via ancestor check; skip to avoid infinite traversal.
					continue
				}
				if visitedSymlinks[realPath] {
					// Symlink cycle detected via visited set; skip to avoid infinite traversal.
					continue
				}
				visitedSymlinks[realPath] = true
				// Follow the symlink to get the target's file info.
				info, err = os.Stat(realPath)
				if err != nil {
					// Symlink target inaccessible; skip silently.
					continue
				}
				// If the symlink target is outside writable directories,
				// mark this path as read-only. If the target is a directory,
				// also record it so files discovered under it inherit the
				// read-only status.
				if isOutsideWritableDirs(realPath) {
					readOnly = true
					if info.IsDir() {
						externalSymlinkDirs[path] = true
					}
				}
			}

			// For directly-matched patterns, mark paths outside writable
			// directories as read-only. The writable directories match
			// the security package's container filesystem policy.
			// See TheoryOfFocusFileDirectoryCheck.
			if item.directMatch {
				outside, err := pathutil.IsOutsideWritableDirs(path)
				if err != nil {
					yield(FileInfo{}, fmt.Errorf("check focus file path: %w", err))
					return
				}
				if outside {
					readOnly = true
				}
			}

			if info.IsDir() {
				entries, err := os.ReadDir(path)
				if err != nil {
					yield(FileInfo{}, err)
					return
				}
				// Sort entries by name for deterministic ordering across filesystems.
				slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
					return cmp.Compare(a.Name(), b.Name())
				})
				for _, entry := range entries {
					queue = append(queue, queueItem{
						path:        filepath.Join(path, entry.Name()),
						directMatch: false,
					})
				}
				continue
			}

			// plain file
			if !c.FileNameOK()(path) {
				continue
			}
			if !c.NameMatch()(path) {
				continue
			}

			candidates = append(candidates, candidate{
				path:     path,
				modTime:  info.ModTime(),
				readOnly: readOnly,
			})
		}

		// Sort files by path as the primary key for deterministic ordering.
		// See TheoryOfFileOrdering for rationale.
		slices.SortStableFunc(candidates, func(a, b candidate) int {
			if a.path != b.path {
				return cmp.Compare(a.path, b.path)
			}
			if a.modTime.Before(b.modTime) {
				return -1
			} else if b.modTime.Before(a.modTime) {
				return 1
			}
			return 0
		})

		// Process candidates in sorted order
		for _, cand := range candidates {
			content, err := os.ReadFile(cand.path)
			if err != nil {
				yield(FileInfo{}, err)
				return
			}

			// mime type
			mtype := mimetype.Detect(content)
			ok := false
			isText := false
		loop:
			for t := mtype; t != nil; t = t.Parent() {
				if t.Is("text/plain") {
					ok = true
					isText = true
					break
				}
				for m := range c.IncludeMimeTypes() {
					if t.Is(m) {
						ok = true
						break loop
					}
				}
			}

			if !ok {
				continue
			}

			if !yield(FileInfo{
				Path:     cand.path,
				Content:  content,
				IsText:   isText,
				MimeType: mtype.String(),
				ModTime:  cand.modTime,
				ReadOnly: cand.readOnly,
			}, nil) {
				return
			}
		}
	}
}

// isAncestor reports whether ancestor is an ancestor directory of the parent
// of path in the real filesystem. Both paths are resolved to their canonical
// forms before comparison. This is used to detect symbolic link cycles: if a
// symlink's target is an ancestor of the current path, following the link
// would revisit a directory already on the traversal path.
func isAncestor(ancestor, path string) bool {
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		parentReal = filepath.Dir(path)
	}
	ancestor = filepath.Clean(ancestor)
	parentReal = filepath.Clean(parentReal)
	if ancestor == parentReal {
		return true
	}
	rel, err := filepath.Rel(ancestor, parentReal)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// isOutsideWritableDirs reports whether the given path is outside all
// writable directories. It delegates to pathutil.IsOutsideWritableDirs,
// which checks against the security package's container filesystem policy.
// Errors are treated as "not outside" (writable) to avoid false read-only
// markings on unresolvable paths. See security.TheoryOfWritableDirs.
func isOutsideWritableDirs(path string) bool {
	outside, err := pathutil.IsOutsideWritableDirs(path)
	if err != nil {
		return false
	}
	return outside
}

// isUnderExternalDir checks whether the given path is under a directory that
// was introduced via a symlink to an external location. Files under such
// directories inherit the read-only status from the directory symlink.
func isUnderExternalDir(path string, externalDirs map[string]bool) bool {
	dir := filepath.Dir(path)
	for dir != "." && dir != "/" && dir != "" {
		if externalDirs[dir] {
			return true
		}
		dir = filepath.Dir(dir)
	}
	return false
}

// WorkingDirectoryPart returns a Text part carrying the absolute path of
// the process working directory, or nil when the directory cannot be
// determined. PartsProvider.Parts appends it after all file contents so
// the model can construct correct absolute paths for change block
// file-path attributes. It is exported because
// gotools.PartsProvider.Parts appends the same hint after the Go file
// contents. See TheoryOfWorkingDirectoryHint.
func WorkingDirectoryPart() generators.Part {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return generators.Text(
		"Working directory: " + cwd + "\n" +
			"Construct change block file-path attributes as absolute paths within this working directory.\n",
	)
}

// isExcludedPath checks whether the given path is excluded by any exclusion
// pattern. Non-glob patterns are treated as directory prefixes: "pkg"
// excludes both "pkg" itself and everything under "pkg/". Glob patterns
// are matched with doublestar.PathMatch. The path is also matched with
// leading ".." components stripped, so patterns are evaluated against the
// project or workspace root rather than the current position. Slash-less
// patterns (e.g., "*.md" or "README.md") additionally match the path's
// basename at any depth, following gitignore-style semantics.
// See TheoryOfPatternMatching.
func isExcludedPath(path string, excludePatterns []string) bool {
	cleanedPath := filepath.Clean(path)
	baseName := filepath.Base(cleanedPath)

	// Strip leading ".." components so patterns are matched against the
	// path relative to the project or workspace root. A file in a sibling
	// workspace module has path "../mod2/README.md"; the pattern
	// "mod2/README.md" must still match it. See TheoryOfPatternMatching.
	trimmedPath := cleanedPath
	for strings.HasPrefix(trimmedPath, ".."+string(filepath.Separator)) {
		trimmedPath = strings.TrimPrefix(trimmedPath, ".."+string(filepath.Separator))
	}

	for _, pattern := range excludePatterns {
		cleanedPattern := filepath.Clean(pattern)

		// Glob match against the raw path and the path with leading
		// ".." stripped.
		if matched, err := doublestar.PathMatch(pattern, path); err == nil && matched {
			return true
		}
		if matched, err := doublestar.PathMatch(pattern, trimmedPath); err == nil && matched {
			return true
		}

		// Directory-prefix match for non-glob patterns: "pkg" excludes
		// both "pkg" itself and everything under "pkg/".
		if !strings.ContainsAny(cleanedPattern, "*?[") &&
			(cleanedPath == cleanedPattern ||
				strings.HasPrefix(cleanedPath, cleanedPattern+string(filepath.Separator)) ||
				trimmedPath == cleanedPattern ||
				strings.HasPrefix(trimmedPath, cleanedPattern+string(filepath.Separator))) {
			return true
		}

		// Slash-less patterns match the basename at any depth, so files
		// in subdirectories or sibling workspace modules are excluded by
		// name or extension.
		if !strings.Contains(cleanedPattern, string(filepath.Separator)) {
			if matched, err := doublestar.PathMatch(cleanedPattern, baseName); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func (c PartsProvider) Parts(
	maxTokens int,
	countTokens func(string) (int, error),
	patterns []string,
) (
	parts []generators.Part,
	err error,
) {
	// Separate inclusion and exclusion patterns. Exclusion patterns use a
	// "!" prefix; they are not file paths and must not be passed to IterFiles,
	// which would attempt to os.Lstat them and abort iteration on error.
	var includePatterns, excludePatterns []string
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			excludePatterns = append(excludePatterns, p[1:])
		} else {
			includePatterns = append(includePatterns, p)
		}
	}

	totalTokens := 0
	for info, err := range c.IterFiles(includePatterns) {
		if err != nil {
			return nil, err
		}

		// Skip files excluded by patterns.
		if isExcludedPath(info.Path, excludePatterns) {
			continue
		}

		if info.IsText {

			readOnlyNote := ""
			if info.ReadOnly {
				readOnlyNote = " (read-only)"
			}
			text := "``` begin of file " + info.Path + readOnlyNote + "\n" +
				string(info.Content) + "\n" +
				"``` end of file " + info.Path + "\n"

			numTokens, err := countTokens(text)
			if err != nil {
				return nil, err
			}
			if totalTokens+numTokens > maxTokens {
				c.Logger().Info("file skipped due to token limit",
					"at file", info.Path,
					"file tokens", numTokens,
					"total tokens", totalTokens,
					"max tokens", maxTokens,
				)
				break
			}
			totalTokens += numTokens

			parts = append(parts, generators.Text(text))

			if c.Debug() {
				c.Logger().Info("text file",
					"path", info.Path,
					"tokens", numTokens,
					"mime type", info.MimeType,
					"read only", info.ReadOnly,
				)
			}

		} else {
			// Binary files are wrapped with begin/end markers matching the text
			// file format so the model can identify the attachment boundary.
			// See TheoryOfBinaryFileMarkers.
			readOnlyNote := ""
			if info.ReadOnly {
				readOnlyNote = ", read-only"
			}
			beginMarker := "``` begin of file " + info.Path + " (binary, " + info.MimeType + ")" + readOnlyNote + "\n"
			endMarker := "\n``` end of file " + info.Path + "\n"

			// Count text markers for the token budget. Binary content itself
			// cannot be accurately counted by a text tokenizer, but the markers
			// are text and must be accounted for to prevent budget overflow.
			markerTokens, err := countTokens(beginMarker + endMarker)
			if err != nil {
				return nil, err
			}
			if totalTokens+markerTokens > maxTokens {
				c.Logger().Info("binary file skipped due to token limit",
					"at file", info.Path,
					"marker tokens", markerTokens,
					"total tokens", totalTokens,
					"max tokens", maxTokens,
				)
				break
			}
			totalTokens += markerTokens

			parts = append(parts, generators.Text(beginMarker))
			parts = append(parts, generators.FileContent{
				Content:  info.Content,
				MimeType: info.MimeType,
			})
			parts = append(parts, generators.Text(endMarker))

			if c.Debug() {
				c.Logger().Info("binary file",
					"path", info.Path,
					"mime type", info.MimeType,
					"read only", info.ReadOnly,
				)
			}

		}

	}

	// The working directory hint is appended after all file contents so
	// the model can construct correct absolute paths for change block
	// file-path attributes. The path is dynamic — it changes per
	// invocation — so it is placed at the end, keeping the file contents
	// byte-identical in the LLM prefix cache across runs in different
	// directories. See TheoryOfWorkingDirectoryHint.
	if part := WorkingDirectoryPart(); part != nil {
		parts = append(parts, part)
	}

	c.Logger().Info("anytexts.PartsProvider",
		"max tokens", maxTokens,
		"total tokens", totalTokens,
	)

	return
}

func (Module) PartsProvider(
	inject dscope.InjectStruct,
) (ret PartsProvider) {
	inject(&ret)
	return
}
