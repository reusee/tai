package anytexts

import (
	"cmp"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
)

const TheoryOfSymlinkTraversal = `
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
`

const TheoryOfReadOnlySymlinks = `
Files introduced via symbolic links that point outside the current working
directory are marked as read-only in the file context markers. The model is
warned not to modify these files because they reside outside the project tree
and attempting to write to them may cause permission errors or unintended
modifications to external files. This applies both to files that are direct
symlinks to external locations and to files discovered inside directories
that are symlinks to external locations. Symlinks to files or directories
within the current directory are not marked as read-only since they are part
of the project.
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

const TheoryOfBinaryFileMarkers = `
Binary files included in the model context must be wrapped with begin/end markers
matching the text file format, so the model can identify the attachment boundary.
The marker includes the MIME type to help the model understand the content type.
Without end markers, the model cannot determine where the binary attachment ends
and the next file's content begins, especially when multiple binary files are
included consecutively.
`

type CodeProvider struct {
	FileNameOK dscope.Inject[FileNameOK]
	NameMatch  dscope.Inject[NameMatch]
	Logger     dscope.Inject[logs.Logger]
}

var _ codetypes.CodeProvider = CodeProvider{}

type FileInfo struct {
	Path     string
	Content  []byte
	IsText   bool
	MimeType string
	ModTime  time.Time
	ReadOnly bool
}

func (c CodeProvider) IterFiles(patterns []string) iter.Seq2[FileInfo, error] {
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
		var queue []string

		for _, pattern := range patterns {
			files, err := filepath.Glob(pattern)
			if err != nil {
				// use as-is
				queue = append(queue, pattern)
			} else {
				slices.SortStableFunc(files, cmp.Compare[string])
				queue = append(queue, files...)
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
			path := queue[0]
			queue = queue[1:]

			baseName := filepath.Base(path)
			// ignore hidden files
			if baseName != "." && strings.HasPrefix(baseName, ".") {
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
				// If the symlink target is outside the current directory,
				// mark this path as read-only. If the target is a directory,
				// also record it so files discovered under it inherit the
				// read-only status.
				if isOutsideCurrentDir(realPath) {
					readOnly = true
					if info.IsDir() {
						externalSymlinkDirs[path] = true
					}
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
					queue = append(queue, filepath.Join(path, entry.Name()))
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
				for m := range includeNonTextMimeTypes {
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

// isOutsideCurrentDir reports whether the given path is outside the current
// working directory. It is used to detect symbolic links that point to files
// or directories outside the project tree, which should be treated as read-only.
//
// The path argument is expected to be already canonicalized (e.g. via
// filepath.EvalSymlinks), so the working directory must be canonicalized the
// same way before comparison. On platforms where the working directory
// contains symlink components (such as macOS, where /var is a symlink to
// /private/var), os.Getwd returns the logical path while the path argument
// is canonical. Comparing a logical path against a canonical path makes
// filepath.Rel produce a ".."-prefixed result even for internal targets,
// falsely marking them as external and read-only.
//
// filepath.EvalSymlinks returns a relative path when the input is relative.
// The path is converted to absolute (joined with the canonicalized working
// directory) before comparison so that filepath.Rel can compute a valid
// relative path; otherwise mixing an absolute cwd with a relative path
// causes filepath.Rel to error and falsely classify internal targets as
// external.
func isOutsideCurrentDir(path string) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return true
	}
	return strings.HasPrefix(rel, "..")
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

// isExcludedPath checks whether the given path is excluded by any exclusion
// pattern. Non-glob patterns are treated as directory prefixes: "pkg"
// excludes both "pkg" itself and everything under "pkg/". Glob patterns
// are matched with filepath.Match.
func isExcludedPath(path string, excludePatterns []string) bool {
	cleanedPath := filepath.Clean(path)
	for _, pattern := range excludePatterns {
		cleanedPattern := filepath.Clean(pattern)
		if cleanedPath == cleanedPattern {
			return true
		}
		if !strings.ContainsAny(cleanedPattern, "*?[") &&
			strings.HasPrefix(cleanedPath, cleanedPattern+string(filepath.Separator)) {
			return true
		}
		if matched, err := filepath.Match(pattern, path); err == nil && matched {
			return true
		}
	}
	return false
}

func (c CodeProvider) Parts(
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

			if *debug {
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

			if *debug {
				c.Logger().Info("binary file",
					"path", info.Path,
					"mime type", info.MimeType,
					"read only", info.ReadOnly,
				)
			}

		}

	}

	c.Logger().Info("anytexts.CodeProvider",
		"max tokens", maxTokens,
		"total tokens", totalTokens,
	)

	return
}

func (c CodeProvider) Functions() []*generators.Function {
	return nil
}

func (c CodeProvider) SystemPrompt() string {
	return ""
}

func (Module) CodeProvider(
	inject dscope.InjectStruct,
) (ret CodeProvider) {
	inject(&ret)
	return
}
