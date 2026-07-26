package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

const TheoryOfPathSafety = `
Path safety checks prevent directory traversal by distinguishing
parent-directory traversal (".." and "../"-prefixed paths) from valid
directory or file names that merely start with two dots (e.g.,
"..hidden", "..."). The check is shared across packages that handle
model-supplied or user-supplied file paths, ensuring consistent safety
semantics regardless of the path source.
`

// EscapesDir reports whether a cleaned relative path escapes the current
// directory via parent-directory traversal. It distinguishes ".." (parent
// directory) and "../"-prefixed paths from names that merely start with
// two dots (e.g., "..hidden", "..."), which are valid directory or file
// names.
func EscapesDir(cleaned string) bool {
	return cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// RootMkdirAll creates a directory path within an os.Root, creating parent
// directories as needed. It is equivalent to os.MkdirAll but operates
// within the restricted filesystem root.
func RootMkdirAll(root *os.Root, path string, perm os.FileMode) error {
	path = filepath.Clean(path)
	if path == "." || path == "/" || path == "" {
		return nil
	}
	err := root.Mkdir(path, perm)
	if err == nil || os.IsExist(err) {
		return nil
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := RootMkdirAll(root, parent, perm); err != nil {
			return err
		}
	}
	return root.Mkdir(path, perm)
}
