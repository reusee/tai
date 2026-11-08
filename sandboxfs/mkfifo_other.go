//go:build !linux

package sandboxfs

import "os"

// mkfifoAt reports that this platform has no root-confined mknodat operation.
// A path-based mknod fallback could escape the root through a racing symlink,
// so the apply reports the unsupported operation instead of weakening the
// store's containment rule. See TheoryOfSandboxFilesystem.
func mkfifoAt(root *os.Root, rel string, mode os.FileMode) error {
	return ErrUnsupported
}
