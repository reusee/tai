//go:build linux

package security

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const TheoryOfContainerIsolation = `
The tai command runs in a Linux user namespace (CLONE_NEWUSER, CLONE_NEWNS,
CLONE_NEWPID, CLONE_NEWUTS, and CLONE_NEWIPC) to isolate filesystem access,
preventing AI-driven code generation from writing outside the intended project
boundary. Network namespace is deliberately omitted because the AI pipeline
requires network access to call model APIs. The process re-executes itself in
the new namespace on first launch; the inContainerEnv environment variable marks
that the process is already containerized, ensuring re-execution happens only
once. On non-Linux platforms, container isolation is a no-op and the command
runs directly.

Filesystem hardening enforces a read-only-everything policy with targeted
writable exceptions. All mount points are remounted read-only, then the current
working directory is bind-mounted onto itself read-write, creating a writable
enclave for project file modifications. Go toolchain directories (GOCACHE,
GOMODCACHE, GOPATH/pkg) are resolved before namespace creation and individually
bind-mounted read-write so the Go toolchain can function (build cache, module
downloads, package objects). The user config directory is also resolved before
namespace creation and bind-mounted read-write so the memory system
(ai-memory.json) and chat history (ai-chat-history.json) can persist data across
sessions. Without this exception, the read-only filesystem would cause all writes
to the config directory to fail silently, losing memory updates. A fresh tmpfs
is mounted on /tmp for isolated temporary file storage, and on /dev/shm for
isolated shared memory. /proc is remounted to show only namespace-local
processes, /sys is made read-only, and sensitive /proc paths are masked with
bind-mounted /dev/null. The NO_NEW_PRIVS prctl flag prevents privilege
escalation through exec, complementing the user namespace's capability
restrictions.
`

// disableContainerEnv allows bypassing containerization entirely for debugging
// or pre-isolated environments (e.g., CI runners that already provide sandboxing).
// See TheoryOfContainerIsolation.
const disableContainerEnv = "CAI_DISABLE_CONTAINER"

// goWritableDirsEnv carries the colon-separated list of Go toolchain directories
// that should be bind-mounted read-write inside the container. These are resolved
// before entering the namespace because `go env` may not function correctly after
// mount restrictions are applied. See TheoryOfContainerIsolation.
const goWritableDirsEnv = "CAI_GO_WRITABLE_DIRS"

// configDirEnv carries the user config directory path that should be
// bind-mounted read-write inside the container. This is resolved before
// entering the namespace because os.UserConfigDir may not function
// correctly after mount restrictions are applied. The memory system
// (ai-memory.json) and chat history (ai-chat-history.json) persist
// files in this directory. See TheoryOfContainerIsolation.
const configDirEnv = "CAI_CONFIG_DIR"

// tmpTmpfsSizeEnv carries the size limit for the /tmp tmpfs mount inside
// the container. The value is passed through os.Environ() during re-exec.
// When unset, defaultTmpTmpfsSize is used. This is configurable to prevent
// false ENOSPC errors when tests create large temporary files that exceed
// the default cap. See TheoryOfContainerIsolation.
const tmpTmpfsSizeEnv = "CAI_TMP_TMPFS_SIZE"

// shmTmpfsSizeEnv carries the size limit for the /dev/shm tmpfs mount
// inside the container. The value is passed through os.Environ() during
// re-exec. When unset, defaultShmTmpfsSize is used.
// See TheoryOfContainerIsolation.
const shmTmpfsSizeEnv = "CAI_SHM_TMPFS_SIZE"

// defaultTmpTmpfsSize is the default size for the /tmp tmpfs mount.
const defaultTmpTmpfsSize = "256m"

// defaultShmTmpfsSize is the default size for the /dev/shm tmpfs mount.
const defaultShmTmpfsSize = "64m"

// inContainerEnv marks that the process is already running inside the
// container namespace, ensuring re-execution happens only once.
// See TheoryOfContainerIsolation.
const inContainerEnv = "CAI_IN_CONTAINER"

// prSetNoNewPrivs is the prctl option number for PR_SET_NO_NEW_PRIVS on Linux.
// It prevents the process and its descendants from gaining new privileges
// through exec (e.g., setuid binaries). See TheoryOfContainerIsolation.
const prSetNoNewPrivs = 38

// MaybeRunInContainer re-executes the current process inside a new user
// namespace with filesystem isolation, or hardens the filesystem if already
// containerized. On non-Linux platforms this is a no-op.
// See TheoryOfContainerIsolation.
func MaybeRunInContainer() {
	// Allow disabling containerization for debugging or restricted kernels.
	if os.Getenv(disableContainerEnv) != "" {
		return
	}

	if os.Getenv(inContainerEnv) == "" {
		// Not yet containerized; re-exec in new namespaces.
		args := os.Args
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		env := append(os.Environ(), inContainerEnv+"=1")
		// Resolve Go toolchain writable directories before entering the
		// namespace, since `go env` may not work correctly after mount
		// restrictions are applied. See TheoryOfContainerIsolation.
		goDirs := resolveGoWritableDirs()
		if len(goDirs) > 0 {
			env = append(env, goWritableDirsEnv+"="+strings.Join(goDirs, string(filepath.ListSeparator)))
		}
		// Resolve user config directory before entering the namespace,
		// since os.UserConfigDir may not work correctly after mount
		// restrictions are applied. The memory system (ai-memory.json)
		// and chat history (ai-chat-history.json) persist files in this
		// directory. See TheoryOfContainerIsolation.
		if configDir := resolveConfigDir(); configDir != "" {
			env = append(env, configDirEnv+"="+configDir)
		}
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{
			// User namespace: allows unprivileged container creation.
			// Mount namespace: enables filesystem isolation.
			// PID namespace: prevents host process visibility/interaction.
			// UTS namespace: isolates hostname.
			// IPC namespace: isolates inter-process communication.
			// Network namespace is deliberately omitted: the AI pipeline
			// requires network access to call model APIs.
			Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS |
				syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
			UidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
			GidMappings: []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		}

		if err := cmd.Start(); err != nil {
			// Namespace creation or exec failed (e.g., kernel restricts
			// unprivileged user namespaces). Fall back to running without
			// containerization rather than aborting, so the tool remains
			// usable on locked-down systems. The user can supplement with
			// external sandboxing (e.g., Docker) if needed.
			fmt.Fprintf(os.Stderr, "Warning: container isolation unavailable (%v), continuing without sandbox\n", err)
			return
		}

		// Forward signals that target the parent specifically (not the
		// entire process group) to the containerized child, so that
		// `kill <pid>` and terminal disconnect (SIGHUP) reach the child.
		// SIGINT (Ctrl+C) is delivered to the entire process group
		// automatically and is not forwarded to avoid double delivery.
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGHUP)
		go func() {
			for sig := range sigChan {
				cmd.Process.Signal(sig)
			}
		}()

		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "Warning: container process error (%v)\n", err)
			return
		}
		os.Exit(0)
	}

	// Already in container; harden the filesystem before continuing.
	if err := setupContainerFilesystem(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: container filesystem setup incomplete: %v\n", err)
	}
}

// resolveTmpfsSize returns the tmpfs size from the given environment
// variable, or the default if the variable is unset or empty. Used to
// configure /tmp and /dev/shm tmpfs size limits inside the container.
// See TheoryOfContainerIsolation.
func resolveTmpfsSize(envName, defaultSize string) string {
	if s := os.Getenv(envName); s != "" {
		return s
	}
	return defaultSize
}

// parseMountPoints reads /proc/self/mountinfo and returns a list of mount
// point paths. Used to enumerate mounts for read-only remounting. Each line
// of mountinfo has the mount point as the 5th field (index 4).
func parseMountPoints() ([]string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	var mounts []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		mounts = append(mounts, fields[4])
	}
	return mounts, nil
}

func setupContainerFilesystem() error {
	// 1. Make all mounts private to this namespace. This is critical:
	// without it, subsequent mount operations could propagate to the
	// host mount table via shared subtrees, modifying the host's
	// filesystem view.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	// 2. Parse mount points before making them read-only, so we can
	// enumerate and selectively remount them.
	mountPoints, err := parseMountPoints()
	if err != nil {
		return fmt.Errorf("parse mount points: %w", err)
	}

	// 3. Remount non-special mount points as read-only. This prevents
	// writes to any directory that is not explicitly exempted. Special
	// mounts (proc, sys, dev, tmp) are handled separately below.
	// Failure for individual mounts is non-critical (the mount may not
	// support remount or may already be read-only).
	specialMounts := map[string]bool{
		"/":           true,
		"/proc":       true,
		"/sys":        true,
		"/dev":        true,
		"/dev/pts":    true,
		"/dev/shm":    true,
		"/dev/mqueue": true,
		"/run":        true,
		"/tmp":        true,
	}
	for _, mp := range mountPoints {
		if specialMounts[mp] {
			continue
		}
		syscall.Mount("", mp, "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")
	}

	// 4. Remount root as read-only. This is the primary filesystem
	// restriction: everything under / is read-only unless overridden
	// by a bind mount. Failure is non-critical (root may already be
	// read-only or may not support remount).
	syscall.Mount("", "/", "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")

	// 5. Bind-mount the current working directory onto itself read-write.
	// This creates a new mount that shadows the read-only view of the
	// same directory, allowing the AI to write files only within the
	// project boundary. The bind mount covers the entire CWD subtree,
	// so all files under CWD are writable.
	// A second remount ensures the mount is explicitly read-write even
	// if the source mount was read-only (bind mounts inherit the source
	// mount's read-only flag; the remount clears it).
	if cwd != "" && cwd != "/" {
		if err := syscall.Mount(cwd, cwd, "", syscall.MS_BIND, ""); err == nil {
			syscall.Mount("", cwd, "", syscall.MS_REMOUNT, "")
		} else {
			fmt.Fprintf(os.Stderr, "Warning: failed to make working directory writable in sandbox (%v)\n", err)
		}
	}

	// 6. Bind-mount Go toolchain writable directories read-write.
	// These directories (GOCACHE, GOMODCACHE, GOPATH/pkg) are needed by
	// the Go toolchain for build cache, module downloads, and package
	// objects. Directories already under CWD are skipped (already
	// writable via the CWD bind mount). Failure for individual dirs
	// is non-critical (the directory may not exist or may already be
	// writable).
	goDirsStr := os.Getenv(goWritableDirsEnv)
	for _, dir := range filepath.SplitList(goDirsStr) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" || dir == "/" {
			continue
		}
		// Skip directories already under CWD (already writable).
		if dir == cwd || strings.HasPrefix(dir, cwd+string(filepath.Separator)) {
			continue
		}
		if err := syscall.Mount(dir, dir, "", syscall.MS_BIND, ""); err == nil {
			syscall.Mount("", dir, "", syscall.MS_REMOUNT, "")
		}
	}

	// 7. Bind-mount the user config directory read-write so the memory
	// system (ai-memory.json) and chat history (ai-chat-history.json)
	// can persist files. The directory is resolved before entering the
	// namespace because os.UserConfigDir may not work correctly after
	// mount restrictions are applied. Directories already under CWD are
	// skipped (already writable via the CWD bind mount). Failure is
	// non-critical (the directory may not exist or may already be
	// writable). See TheoryOfContainerIsolation.
	configDir := os.Getenv(configDirEnv)
	if configDir != "" {
		configDir = filepath.Clean(configDir)
		if configDir != "/" && configDir != "" {
			// Skip if already under CWD (already writable via the CWD bind mount).
			if configDir != cwd && !strings.HasPrefix(configDir, cwd+string(filepath.Separator)) {
				if err := syscall.Mount(configDir, configDir, "", syscall.MS_BIND, ""); err == nil {
					syscall.Mount("", configDir, "", syscall.MS_REMOUNT, "")
				}
			}
		}
	}

	// 8. Mount a fresh tmpfs on /tmp. This isolates temporary files from
	// the host and provides a writable /tmp for tools that need it
	// (e.g., go test creates temporary files). The size limit is
	// configurable via tmpTmpfsSizeEnv to prevent false ENOSPC errors
	// when tests create large temporary files that exceed the default
	// cap. Best-effort: if mounting fails, /tmp is either read-only
	// (from the root mount) or whatever it was before.
	_ = syscall.Unmount("/tmp", syscall.MNT_DETACH)
	tmpSize := resolveTmpfsSize(tmpTmpfsSizeEnv, defaultTmpTmpfsSize)
	syscall.Mount("tmpfs", "/tmp", "tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV, "size="+tmpSize)

	// 9. Mount a fresh tmpfs on /dev/shm for shared memory isolation.
	// This prevents the container from accessing host POSIX shared
	// memory segments. The size limit is configurable via
	// shmTmpfsSizeEnv. Best-effort: if /dev/shm doesn't exist or
	// mounting fails, the existing mount (or none) is used.
	_ = syscall.Unmount("/dev/shm", syscall.MNT_DETACH)
	shmSize := resolveTmpfsSize(shmTmpfsSizeEnv, defaultShmTmpfsSize)
	syscall.Mount("tmpfs", "/dev/shm", "tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV, "size="+shmSize)

	// 10. Remount /proc to show only processes in this PID namespace.
	// The host /proc exposes all host PIDs; the remount restricts
	// visibility to the container's process tree. If unmount fails
	// (e.g., /proc is busy), the remount is skipped and the host /proc
	// remains — a security degradation but not a functional break.
	if err := syscall.Unmount("/proc", syscall.MNT_DETACH); err == nil {
		if err := syscall.Mount("proc", "/proc", "proc",
			syscall.MS_NOSUID|syscall.MS_NOEXEC|syscall.MS_NODEV, ""); err != nil {
			// Remount failed after successful unmount; attempt to
			// restore /proc so the process does not run without it.
			syscall.Mount("proc", "/proc", "proc", 0, "")
		}
	}

	// 11. Make /sys read-only to prevent sysfs tampering. Failure is
	// non-critical: /sys may not be a separate mount point on all systems.
	syscall.Mount("", "/sys", "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")

	// 12. Mask sensitive /proc files that could leak kernel memory or
	// configuration. Bind-mount /dev/null over each path so reads return
	// empty content instead of privileged data. Failure for individual
	// paths is non-critical (the path may not exist on all kernels).
	for _, p := range []string{
		"/proc/kcore",
		"/proc/kallsyms",
		"/proc/bus",
		"/proc/config.gz",
		"/proc/sched_debug",
		"/proc/keys",
		"/proc/timer_list",
	} {
		syscall.Mount("/dev/null", p, "", syscall.MS_BIND, "")
	}

	// 13. Set NO_NEW_PRIVS to prevent privilege escalation through exec.
	// This ensures that even if a setuid binary is somehow executed, it
	// cannot gain new privileges. Best-effort: failure is silently
	// ignored since the user namespace already limits capabilities.
	setNoNewPrivs()

	return nil
}

// setNoNewPrivs sets the NO_NEW_PRIVS flag on the current process,
// preventing it and its descendants from gaining new privileges through
// exec. This is a defense-in-depth measure: the user namespace already
// limits capabilities, but NO_NEW_PRIVS ensures setuid binaries cannot
// escalate. Best-effort: failure is silently ignored.
func setNoNewPrivs() {
	_, _, _ = syscall.Syscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0)
}
