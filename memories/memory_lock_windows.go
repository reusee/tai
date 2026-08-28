//go:build windows

package memories

import "os"

// On Windows, os.FindProcess opens a handle to the process and fails for
// a dead PID, so it doubles as the liveness probe; signal 0 is not a
// Windows concept. See TheoryOfMemory.
func signalZero(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
