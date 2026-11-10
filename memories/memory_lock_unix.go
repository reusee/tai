//go:build unix

package memories

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile attempts to take an exclusive advisory lock on f without
// blocking, and reports whether it holds the lock. The kernel owns the lock:
// it is dropped when the holder exits, however it exits, so a lock left by a
// crashed writer never blocks a later one and no PID liveness check is
// needed. Two open file descriptions of one path conflict even inside one
// process, so a retried acquisition observes a concurrent holder the same way
// another process does. See TheoryOfMemory.
func tryLockFile(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

// unlockFile releases the lock tryLockFile took and closes f. The lock file
// stays on disk: it is the rendezvous point every writer opens, and removing
// it would let one writer lock an unlinked inode while a later writer locks a
// fresh file at the same path. See TheoryOfMemory.
func unlockFile(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
}
