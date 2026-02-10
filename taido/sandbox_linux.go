//go:build linux

package taido

import (
	"fmt"
	"unsafe"

	"github.com/reusee/tai/logs"
	"golang.org/x/sys/unix"
)

// applySandbox uses Linux Landlock to restrict the process to write-access in the current directory only.
// Read access remains unrestricted across the filesystem.
func applySandbox(logger logs.Logger) error {
	// 1. Get Landlock ABI version to check support and supported features
	abi, _, errNo := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION,
	)
	if errNo != 0 {
		// If the syscall is not found or Landlock is disabled, we log a warning and continue.
		// This is necessary for environments like CI, WSL, or older kernels.
		if errNo == unix.ENOSYS || errNo == unix.EOPNOTSUPP || errNo == unix.ENOPKG || errNo == unix.EINVAL {
			logger.Warn("landlock not supported or disabled by kernel, running without filesystem sandbox", "error", errNo)
			return nil
		}
		return fmt.Errorf("landlock_create_ruleset(version): %w", errNo)
	}
	if abi < 1 {
		logger.Warn("landlock ABI version is 0, running without filesystem sandbox")
		return nil
	}

	// 2. Define requested rights
	// Basic read rights (ABI v1)
	readRights := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR)

	// Basic write rights (ABI v1)
	writeRights := uint64(unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM)

	// Features added in later ABIs
	if abi >= 2 {
		writeRights |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		writeRights |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}

	// Create ruleset
	rulesetAttr := unix.LandlockRulesetAttr{
		Access_fs: readRights | writeRights,
	}
	ruleset, _, errNo := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&rulesetAttr)),
		unsafe.Sizeof(rulesetAttr),
		0,
	)
	if errNo != 0 {
		return fmt.Errorf("landlock_create_ruleset: %w", errNo)
	}
	defer unix.Close(int(ruleset))

	// Rule 1: Allow reading everywhere
	rootFd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open root: %w", err)
	}
	defer unix.Close(rootFd)
	pathBeneathRoot := unix.LandlockPathBeneathAttr{
		Parent_fd:      int32(rootFd),
		Allowed_access: readRights,
	}
	if _, _, errNo := unix.Syscall(
		unix.SYS_LANDLOCK_ADD_RULE,
		ruleset,
		unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&pathBeneathRoot)),
	); errNo != 0 {
		return fmt.Errorf("add root rule: %w", errNo)
	}

	// Rule 2: Allow reading AND writing in the current working directory
	cwdFd, err := unix.Open(".", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open cwd: %w", err)
	}
	defer unix.Close(cwdFd)
	pathBeneathCwd := unix.LandlockPathBeneathAttr{
		Parent_fd:      int32(cwdFd),
		Allowed_access: readRights | writeRights,
	}
	if _, _, errNo := unix.Syscall(
		unix.SYS_LANDLOCK_ADD_RULE,
		ruleset, unix.LANDLOCK_RULE_PATH_BENEATH,
		uintptr(unsafe.Pointer(&pathBeneathCwd)),
	); errNo != 0 {
		return fmt.Errorf("add cwd rule: %w", errNo)
	}

	// Restrict the process
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl no_new_privs: %w", err)
	}
	if _, _, errNo := unix.Syscall(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		ruleset,
		0, 0,
	); errNo != 0 {
		return fmt.Errorf("landlock_restrict_self: %w", errNo)
	}

	logger.Info("autonomous execution sandbox applied", "abi", abi, "write_scope", ".")
	return nil
}
