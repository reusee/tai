//go:build !windows

package memlimit

import "syscall"

// setMemoryLimit sets the maximum virtual address space (RLIMIT_AS) for the
// process. Once set, the process cannot allocate more virtual memory than the
// limit, causing memory allocation calls (mmap, brk) to fail with ENOMEM.
// See TheoryOfMemoryLimit.
func setMemoryLimit(limit uint64) error {
	rlim := syscall.Rlimit{
		Cur: limit,
		Max: limit,
	}
	return syscall.Setrlimit(syscall.RLIMIT_AS, &rlim)
}
