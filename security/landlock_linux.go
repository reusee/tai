//go:build linux

package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const TheoryOfLandlock = `
Landlock re-enforces the mount sandbox's write-containment at the LSM
layer as defense in depth. The mount policy guarantees containment only
while the mount stack behaves correctly; Landlock rules are enforced by
an independent kernel subsystem and, once applied, cannot be revoked by
the process or its descendants. A flaw in one layer then does not open
the filesystem.

The ruleset mirrors the mount policy exactly: it handles only write-side
filesystem rights (write, remove, make, refer, truncate) and allow-lists
the same directories setupContainerFilesystem made writable — the
working directory, the pre-namespace-resolved Go and config directories
(goWritableDirsEnv, configDirEnv), /tmp, and /dev/shm. Read and execute
rights are never handled: the policy is read-only-everything, and
handling them would deny reading project files and executing toolchain
binaries. IOCTL_DEV is excluded because terminal libraries ioctl
/dev/tty.

Rights beyond the running kernel's ABI are dropped from the handled set
(TRUNCATE needs ABI 3, REFER ABI 2); the mount layer already enforces
those accesses, so an older kernel degrades to the mount-only behavior.
The layer is best-effort, like setNoNewPrivs: a kernel without Landlock
(pre-5.13 or LSM disabled at boot) is skipped silently, and a failure
after the kernel advertised support prints a warning and leaves the
mount sandbox as the only layer. Landlock applies only on the hardened
in-container path, so it never changes behavior relative to the mount
policy — it re-enforces the same policy on a second plane, and a
directory whose rule cannot be added aborts enforcement rather than deny
writes the mounts allow. CAI_DISABLE_LANDLOCK bypasses it for debugging,
mirroring disableContainerEnv. Network restrictions (ABI 4) are out of
scope: the pipeline requires outbound model API access, the same reason
the network namespace is omitted.
`

// disableLandlockEnv bypasses the Landlock layer for debugging or for
// environments where the second enforcement plane breaks a legitimate
// workflow. The mount sandbox remains active. See TheoryOfLandlock.
const disableLandlockEnv = "CAI_DISABLE_LANDLOCK"

// Landlock filesystem access rights, from the stable UAPI bit layout.
// The constants are typed so handled-access arithmetic stays uint64.
const (
	landlockAccessFSExecute    uint64 = 1 << 0
	landlockAccessFSWriteFile  uint64 = 1 << 1
	landlockAccessFSReadFile   uint64 = 1 << 2
	landlockAccessFSReadDir    uint64 = 1 << 3
	landlockAccessFSRemoveDir  uint64 = 1 << 4
	landlockAccessFSRemoveFile uint64 = 1 << 5
	landlockAccessFSMakeChar   uint64 = 1 << 6
	landlockAccessFSMakeDir    uint64 = 1 << 7
	landlockAccessFSMakeReg    uint64 = 1 << 8
	landlockAccessFSMakeSock   uint64 = 1 << 9
	landlockAccessFSMakeFifo   uint64 = 1 << 10
	landlockAccessFSMakeBlock  uint64 = 1 << 11
	landlockAccessFSMakeSym    uint64 = 1 << 12
	landlockAccessFSRefer      uint64 = 1 << 13
	landlockAccessFSTruncate   uint64 = 1 << 14
	landlockAccessFSIoctlDev   uint64 = 1 << 15
)

// landlockCreateRulesetVersion is the LANDLOCK_CREATE_RULESET_VERSION
// flag: with it, landlock_create_ruleset returns the kernel's ABI version
// instead of creating a ruleset.
const landlockCreateRulesetVersion = 1 << 0

// landlockRuleTypePathBeneath is LANDLOCK_RULE_PATH_BENEATH: the rule
// argument is a landlockPathBeneathAttr.
const landlockRuleTypePathBeneath = 1

// landlockRulesetAttr is the UAPI struct landlock_ruleset_attr. Only the
// filesystem field is passed: the kernel accepts sizes from this field's
// end up to its own struct size, so one layout works on every
// Landlock-supported kernel regardless of later ABI additions. The
// syscalls are issued through x/sys/unix raw entry points with these
// local layouts, so enforcement does not depend on the x/sys wrapper
// struct evolving with later ABIs.
type landlockRulesetAttr struct {
	handledAccessFS uint64
}

// landlockPathBeneathAttr is the UAPI struct landlock_path_beneath_attr:
// the rights allowed beneath the directory referenced by parentFd.
type landlockPathBeneathAttr struct {
	allowedAccess uint64
	parentFd      int32
}

// landlockABIVersion returns the running kernel's Landlock ABI version,
// or 0 when Landlock is unavailable (pre-5.13 kernel, filtered syscall,
// or LSM disabled at boot).
func landlockABIVersion() int {
	version, _, errno := syscall.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		0, 0,
		landlockCreateRulesetVersion,
	)
	if errno != 0 {
		return 0
	}
	return int(version)
}

// landlockHandledAccesses returns the write-side filesystem rights the
// ruleset handles at the given ABI version. Reads and execution are
// absent by design — the policy is read-only-everything — as is
// IOCTL_DEV, which terminal libraries exercise on /dev/tty. Rights the
// ABI does not support are dropped; the mount layer already enforces
// them. See TheoryOfLandlock.
func landlockHandledAccesses(abiVersion int) uint64 {
	handled := uint64(landlockAccessFSWriteFile |
		landlockAccessFSRemoveDir |
		landlockAccessFSRemoveFile |
		landlockAccessFSMakeChar |
		landlockAccessFSMakeDir |
		landlockAccessFSMakeReg |
		landlockAccessFSMakeSock |
		landlockAccessFSMakeFifo |
		landlockAccessFSMakeBlock |
		landlockAccessFSMakeSym)
	if abiVersion >= 2 {
		handled |= landlockAccessFSRefer
	}
	if abiVersion >= 3 {
		handled |= landlockAccessFSTruncate
	}
	return handled
}

// landlockWritableDirs returns the directories the ruleset allow-lists:
// the same set setupContainerFilesystem made writable. The Go and config
// directories come from the environment values the pre-namespace parent
// resolved, so the LSM layer mirrors the mount layer by construction and
// `go env` is not re-run inside the container. Directories under the
// working directory are skipped: its rule already covers them.
func landlockWritableDirs(cwd string) []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" || dir == "." || dir == "/" {
			return
		}
		if dir == cwd || strings.HasPrefix(dir, cwd+string(filepath.Separator)) {
			return
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv(goWritableDirsEnv)) {
		add(dir)
	}
	add(os.Getenv(configDirEnv))
	add("/tmp")
	add("/dev/shm")
	if cwd != "" && cwd != "/" {
		dirs = append(dirs, cwd)
	}
	return dirs
}

// applyLandlockFilesystemPolicy re-enforces the mount sandbox's
// write-containment at the LSM layer. Best-effort: a kernel without
// Landlock is skipped silently, like setNoNewPrivs; a failure after the
// kernel advertised support prints a warning and leaves the mount
// sandbox as the only layer. Must run after setupContainerFilesystem,
// whose setNoNewPrivs call is a prerequisite of landlock_restrict_self.
// See TheoryOfLandlock.
func applyLandlockFilesystemPolicy() {
	if os.Getenv(disableLandlockEnv) != "" {
		return
	}
	abiVersion := landlockABIVersion()
	if abiVersion < 1 {
		return
	}
	if err := landlockEnforceWriteContainment(abiVersion); err != nil {
		fmt.Fprintf(os.Stderr,
			"Warning: landlock enforcement unavailable (%v), continuing with mount sandbox only\n", err)
	}
}

// landlockEnforceWriteContainment creates the ruleset, allow-lists the
// writable directories, and restricts the process. It returns an error
// instead of enforcing partially: a missing rule would deny writes the
// mount layer allows, so any rule failure aborts enforcement.
func landlockEnforceWriteContainment(abiVersion int) error {
	handled := landlockHandledAccesses(abiVersion)

	attr := landlockRulesetAttr{handledAccessFS: handled}
	rulesetFd, _, errno := syscall.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return errno
	}
	defer syscall.Close(int(rulesetFd))

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	for _, dir := range landlockWritableDirs(cwd) {
		if err := landlockAllowBeneath(rulesetFd, handled, dir); err != nil {
			return fmt.Errorf("allow %s: %w", dir, err)
		}
	}

	_, _, errno = syscall.Syscall(
		unix.SYS_LANDLOCK_RESTRICT_SELF,
		rulesetFd, 0, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

// landlockAllowBeneath adds a path-beneath rule allowing every handled
// right beneath dir. A directory that cannot be opened does not exist,
// so there is nothing beneath it to allow; the rule is skipped and the
// mount layer's read-only view of it stands.
func landlockAllowBeneath(rulesetFd uintptr, allowed uint64, dir string) error {
	parentFd, err := syscall.Open(dir, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	defer syscall.Close(parentFd)

	attr := landlockPathBeneathAttr{
		allowedAccess: allowed,
		parentFd:      int32(parentFd),
	}
	_, _, errno := syscall.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		rulesetFd,
		landlockRuleTypePathBeneath,
		uintptr(unsafe.Pointer(&attr)),
		0, 0, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
