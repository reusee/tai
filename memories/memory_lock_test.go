package memories

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireMemoryLockIgnoresLeftoverContent(t *testing.T) {
	// A writer killed while the lock file was being prepared leaves the file
	// behind with empty or meaningless content. The kernel drops a dead
	// holder's lock, so the leftover must not block a later writer: under the
	// previous PID-file scheme this case exhausted the retry budget and the
	// memory update was lost. See TheoryOfMemory.
	for _, content := range []string{"", "999999999"} {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "ai-memory.json.lock")
		if err := os.WriteFile(lockPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		unlock, err := acquireMemoryLock(lockPath, 3, time.Millisecond, time.Millisecond)
		if err != nil {
			t.Fatalf("lock content %q blocked acquisition: %v", content, err)
		}
		unlock()
	}
}

func TestAcquireMemoryLockRespectsLiveHolder(t *testing.T) {
	// A lock held by a live writer must not be stolen: the retry loop exists
	// to wait for a concurrent writer's read-modify-write.
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "ai-memory.json.lock")

	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFile(holder)
	if locked, err := tryLockFile(holder); err != nil || !locked {
		t.Fatalf("test holder cannot take the lock: %v", err)
	}

	unlock, err := acquireMemoryLock(lockPath, 2, time.Millisecond, time.Millisecond)
	if err == nil {
		unlock()
		t.Fatal("acquireMemoryLock must fail while a live writer holds the lock")
	}
}

func TestAcquireMemoryLockKeepsRendezvousFile(t *testing.T) {
	// The lock file is the path every writer opens; keeping it makes a later
	// acquisition lock the same inode instead of racing to create a fresh one
	// after an unlink. See TheoryOfMemory.
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "ai-memory.json.lock")

	unlock, err := acquireMemoryLock(lockPath, 1, time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file must stay on disk after unlock: %v", err)
	}

	unlock, err = acquireMemoryLock(lockPath, 1, time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("a released lock must be reacquirable: %v", err)
	}
	unlock()
}
