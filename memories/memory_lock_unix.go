//go:build unix

package memories

import (
	"errors"
	"os"
	"syscall"
)

// signalZero reports whether the process with the given PID is alive by
// sending signal 0, which the kernel resolves without delivering any
// signal: it succeeds for a live PID and fails with ESRCH for a dead one.
// EPERM also proves liveness — the process exists but belongs to another
// user. The PID is resolved in the caller's PID namespace, so the probe
// stays correct inside containers where the /proc mount may reflect a
// different namespace. Probing with process.Signal(os.Signal(nil)) is NOT
// a liveness check: the nil Signal fails Signal's internal type assertion
// and errors regardless of liveness. See TheoryOfMemory.
func signalZero(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
