package pathutil

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/reusee/tai/security"
)

const TheoryOfPathSafety = `
Path safety checks prevent directory traversal by distinguishing
parent-directory traversal (".." and "../"-prefixed paths) from valid
directory or file names that merely start with two dots (e.g.,
"..hidden", "..."). The check is shared across packages that handle
model-supplied or user-supplied file paths, ensuring consistent safety
semantics regardless of the path source.

IsOutsideWritableDirs extends path safety to resolved file paths: it
delegates to security.IsWritablePath, which reports whether a path (after
symlink resolution) is inside one of the writable directories defined by
the security package's container filesystem policy (see
security.TheoryOfWritableDirs for the directory set). Canonicalization via
filepath.EvalSymlinks handles platforms where the working directory contains
symlink components and resolves symlinks in the path argument.
`

// EscapesDir reports whether a cleaned relative path escapes the current
// directory via parent-directory traversal. It distinguishes ".." (parent
// directory) and "../"-prefixed paths from names that merely start with
// two dots (e.g., "..hidden", "..."), which are valid directory or file
// names.
func EscapesDir(cleaned string) bool {
	return cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

// IsOutsideWritableDirs reports whether the given path resolves to a
// location outside all writable directories. The writable directories are
// determined by the security package's container filesystem policy: the
// current working directory, Go toolchain directories (GOCACHE,
// GOMODCACHE, GOPATH/pkg), the user config directory, /tmp, and /dev/shm.
// This ensures the focus file check is consistent with the security
// package's container isolation — no more and no less restrictive.
// See security.TheoryOfWritableDirs and TheoryOfPathSafety.
func IsOutsideWritableDirs(path string) (bool, error) {
	writable, err := security.IsWritablePath(path)
	if err != nil {
		return false, err
	}
	return !writable, nil
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
