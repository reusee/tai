//go:build windows

package memlimit

// setMemoryLimit is a no-op on Windows. Windows does not provide a per-process
// virtual memory limit equivalent to setrlimit(RLIMIT_AS) without using Job
// Objects, which would add significant complexity for marginal benefit.
// See TheoryOfMemoryLimit.
func setMemoryLimit(limit uint64) error {
	return nil
}
