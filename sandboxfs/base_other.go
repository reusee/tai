//go:build !linux

package sandboxfs

// The base descriptor fast path is Linux-only: the O_PATH descriptor and the
// fstatat/openat/pread calls it rests on have no portable equivalent. A store
// on another platform answers every base access through os.Root and keeps the
// same semantics. See TheoryOfBaseFdAccess.

func openBaseDirFd(dir string) int { return -1 }

func closeBaseDirFd(fd int) {}

func baseStatAt(fd int, rel string) (Attr, bool, error) {
	return Attr{}, false, nil
}

func baseReadAt(fd int, rel string, off int64, size int) ([]byte, bool, error) {
	return nil, false, nil
}
