//go:build linux

package sandboxfs

import (
	"os"
	"path"
	"syscall"

	"golang.org/x/sys/unix"
)

// mkfifoAt creates a named pipe beneath root. os.Root has no mkfifo, so the
// node is created through the parent directory's descriptor with MKNODAT,
// keeping the write inside the root.
func mkfifoAt(root *os.Root, rel string, mode os.FileMode) error {
	dir := path.Dir(rel)
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Mknodat(int(f.Fd()), path.Base(rel),
		uint32(syscall.S_IFIFO)|uint32(mode.Perm()), 0)
}
