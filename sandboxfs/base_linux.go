//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const TheoryOfBaseFdAccess = `
The base descriptor removes the portable walk from the read hot path. os.Root
resolves every base access by reopening the path beneath the root, one guard
per component, which is the right price for a writer whose target it must
contain and the wrong price for the read path, where a session touches
thousands of paths it never writes.

One O_PATH descriptor of the base directory answers a metadata lookup with a
single fstatat and a content window with one fstatat, one openat, and one
pread. Following symbolic links while reading is the design's existing rule —
the base is read through unchanged — and the descriptor is opened read-only, so
a leak of the descriptor cannot widen write containment. Writes keep the
os.Root walk, because containment matters exactly where the store writes.

The window read checks the host kind before it opens, matching the os.Root
path's order: a directory reports EISDIR, a link or pipe EINVAL, and a regular
file serves its window. A store without the descriptor falls back to the
os.Root path unchanged, so the fast path is an optimization, never a
requirement.
`

// openBaseDirFd opens the read-through base directory once: one O_PATH
// descriptor serves every base stat and window read for the store's life.
// A negative result means the platform offers no such path.
// See TheoryOfBaseFdAccess.
func openBaseDirFd(dir string) int {
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1
	}
	return fd
}

// closeBaseDirFd releases the base descriptor, if one was opened.
func closeBaseDirFd(fd int) {
	if fd >= 0 {
		_ = unix.Close(fd)
	}
}

// baseStatAt reports the attributes of rel beneath fd with one fstatat, never
// following a final-component link, so a link reports itself as the store must.
// ok reports whether the fast path handled the lookup; a false result sends the
// caller back to the os.Root walk. See TheoryOfBaseFdAccess.
func baseStatAt(fd int, rel string) (a Attr, ok bool, err error) {
	if fd < 0 {
		return Attr{}, false, nil
	}
	var st unix.Stat_t
	if statErr := unix.Fstatat(fd, rel, &st, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		return Attr{}, true, statErr
	}
	return attrOfStatT(&st), true, nil
}

// attrOfStatT renders one kernel stat buffer as a sandbox attribute. It reads
// the type bits and the permission bits in one pass, without the reflection and
// allocation an os.FileInfo costs. See TheoryOfBaseFdAccess.
func attrOfStatT(st *unix.Stat_t) Attr {
	kind := KindRegular
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		kind = KindDir
	case unix.S_IFLNK:
		kind = KindSymlink
	case unix.S_IFIFO:
		kind = KindFIFO
	}
	return Attr{
		Kind:    kind,
		Mode:    os.FileMode(st.Mode & 0o777),
		Size:    int64(st.Size),
		ModTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
	}
}

// baseReadAt returns the content window [off, off+size) of rel beneath fd. The
// host kind is checked before the open, so the errors match the os.Root path:
// a directory reports EISDIR and a link or pipe EINVAL. ok reports whether the
// fast path handled the read. See TheoryOfBaseFdAccess.
func baseReadAt(fd int, rel string, off int64, size int) (data []byte, ok bool, err error) {
	if fd < 0 {
		return nil, false, nil
	}
	var st unix.Stat_t
	if statErr := unix.Fstatat(fd, rel, &st, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		return nil, true, statErr
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		return nil, true, syscall.EISDIR
	case unix.S_IFREG:
	default:
		return nil, true, syscall.EINVAL
	}
	f, openErr := unix.Openat(fd, rel, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if openErr != nil {
		return nil, true, openErr
	}
	defer unix.Close(f)
	buf := make([]byte, size)
	n := 0
	for {
		n, err = unix.Pread(f, buf, off)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, true, err
	}
	return buf[:n], true, nil
}
