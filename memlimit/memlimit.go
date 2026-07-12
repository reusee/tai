package memlimit

import (
	"fmt"
	"os"

	"github.com/reusee/tai/cmds"
)

const TheoryOfMemoryLimit = `
Process memory limiting uses OS-level resource controls to prevent runaway memory
consumption. On Unix-like systems, setrlimit(RLIMIT_AS) caps the virtual address
space, causing mmap/brk to fail with ENOMEM when the limit is reached. This is a
hard limit that cannot be exceeded. On Windows, no equivalent mechanism is
available without Job Objects, so the limit is a no-op. The default limit (8 GB)
is chosen to be generous enough for normal operation while preventing OOM-killer
involvement on typical development machines. The -mem-limit flag accepts a value
in megabytes; 0 or unset means use the default.
`

// DefaultLimit is the default memory limit in bytes (8 GB).
const DefaultLimit = 8 * 1024 * 1024 * 1024

// memLimitFlag is the -mem-limit command flag, in megabytes.
// 0 or unset means use DefaultLimit. Positive means use that many MB.
var memLimitFlag = cmds.Var[int64]("-mem-limit")

// ApplyFromFlag sets the process memory limit based on the -mem-limit flag.
// The flag value is in megabytes (MB). If the flag is not set or is 0, the
// default limit (8 GB) is used. On platforms where memory limiting is not
// supported, this is a no-op. A failure to set the limit prints a warning to
// stderr but does not abort, because the limit is a safety net rather than a
// critical requirement.
func ApplyFromFlag() {
	limit := uint64(DefaultLimit)
	if *memLimitFlag > 0 {
		limit = uint64(*memLimitFlag) * 1024 * 1024
	}
	if err := setMemoryLimit(limit); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set memory limit: %v\n", err)
	}
}

// Apply sets the process memory limit to the given number of bytes.
// On platforms where memory limiting is not supported, this is a no-op.
func Apply(limit uint64) error {
	return setMemoryLimit(limit)
}
