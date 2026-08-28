package memories

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAcquireMemoryLockRespectsLiveHolder(t *testing.T) {
	// A lock file held by a live process must not be stolen: stale
	// detection exists to clear the locks of dead holders only, so a
	// concurrent live writer's read-modify-write is never interleaved.
	// See TheoryOfMemory.
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "ai-memory.json.lock")

	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}

	unlock, err := acquireMemoryLock(lockPath, 2, time.Millisecond, time.Millisecond)
	if err == nil {
		unlock()
		t.Fatal("acquireMemoryLock must fail while a live process holds the lock")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("live holder's lock file must survive the failed acquire: %v", err)
	}
}

func TestAcquireMemoryLockRemovesStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "ai-memory.json.lock")

	// A process that exited and was reaped leaves a stale lock behind;
	// acquiring it must remove the stale file and succeed promptly.
	cmd := exec.Command("sleep", "0")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}

	unlock, err := acquireMemoryLock(lockPath, 2, time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("stale lock must be removed and acquired: %v", err)
	}
	unlock()
}
