//go:build linux

package sandboxfs

import (
	"errors"
	"io"
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
pread. The descriptor never follows the final link of a path it reads, the
same rule the os.Root walk keeps: a link reports itself as a link, and the
store serves content only for a regular file. The kernel resolves a link on
its own and asks the store again for the target's path. The descriptor is
opened read-only, so a leak of it cannot widen write containment. Writes keep
the os.Root walk, because containment matters exactly where the store writes.

A directory listing opens the directory once through the root and takes the
enumeration position from that handle, so the windows one listing delivers
resume instead of reopening. Each entry is one fstatat beneath that same
handle, so a window costs its entry count and never that count times the
directory's depth. The entry stat stays because a dirent carries the entry's
type and a READDIRPLUS reply carries its full attribute — the mode, size, and
time a name alone does not hold — so the saving is the walk, not the stat.

The window read checks the host kind before it opens, matching the os.Root
path's order: a directory reports EISDIR, a link or pipe EINVAL, and a regular
file serves its window. A read handle opens its base file through the same
kind check and the same single openat, so streaming a file pays no walk per
open and a pipe stays unopened. A whole-file read takes the same open, so the
store's own read API pays no walk either. A store without the descriptor falls
back to the os.Root path unchanged, so the fast path is an optimization, never
a requirement.
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

// baseDirEntryStat reports one directory entry's attributes relative to the
// open directory dir: one fstatat per entry instead of a resolution of the
// entry's whole path, so one window of a deep directory costs its entry count
// and never that count times the depth. The never-follow rule holds: a link
// reports itself and is never reported as its target. ok reports whether the
// platform serves the call; a false result sends the caller back to the base
// path. See TheoryOfBaseFdAccess and TheoryOfDirectoryStream.
func baseDirEntryStat(dir *os.File, name string) (a Attr, ok bool, err error) {
	if dir == nil {
		return Attr{}, false, nil
	}
	var st unix.Stat_t
	if statErr := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		return Attr{}, true, statErr
	}
	return attrOfStatT(&st), true, nil
}

// attrOfStatT renders one kernel stat buffer as a sandbox attribute. It reads
// the type bits and the permission bits in one pass, without the reflection and
// allocation an os.FileInfo costs. The seconds and nanoseconds are converted
// explicitly: on the 32-bit targets this file builds for, a Timespec holds
// int32 fields, and time.Unix takes int64. See TheoryOfBaseFdAccess.
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
		ModTime: time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec)),
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

// baseOpenFile opens one base file beneath fd for a read handle: one fstatat
// and one openat, the same pair the window read pays. The host kind is checked
// before the open, so a directory reports EISDIR and a link or pipe EINVAL, and
// a pipe is never opened, so its read never blocks. O_NOFOLLOW keeps the rule
// the stat path keeps: a link reports itself and is never opened as content.
// ok reports whether the fast path handled the open; a false result sends the
// caller back to the os.Root walk unchanged. See TheoryOfBaseFdAccess.
func baseOpenFile(fd int, rel string) (f *os.File, ok bool, err error) {
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
	fileFd, openErr := unix.Openat(fd, rel, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if openErr != nil {
		// The stat above named a regular file, so a link here appeared between
		// the two calls: it still reports EINVAL, never its target's bytes.
		if openErr == unix.ELOOP {
			return nil, true, syscall.EINVAL
		}
		return nil, true, openErr
	}
	file := os.NewFile(uintptr(fileFd), rel)
	if file == nil {
		_ = unix.Close(fileFd)
		return nil, true, syscall.EBADF
	}
	return file, true, nil
}

// baseReadFile returns one base file's whole content through the base
// descriptor: the same kind check and the same single open the window read
// pays, so the store's read API costs no component-by-component walk either,
// and a directory or a link reports the error the API answers instead of being
// opened. The read is bounded by the store's own limit, because the whole-file
// callers — the rename and the hard link among them — turn the bytes into a
// stored row: a file above maxFileSize reports EFBIG rather than an allocation
// the store could never hold. ok reports whether the fast path handled the
// read; a false result sends the caller back to the os.Root walk unchanged.
// See TheoryOfBaseFdAccess and TheoryOfBaseCapture.
func baseReadFile(fd int, rel string) (data []byte, ok bool, err error) {
	f, ok, err := baseOpenFile(fd, rel)
	if !ok || err != nil {
		return nil, ok, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(data)) > maxFileSize {
		return nil, true, syscall.EFBIG
	}
	return data, true, nil
}
