//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// disableContainerEnv allows bypassing containerization entirely for debugging
// or pre-isolated environments (e.g., CI runners that already provide sandboxing).
// See TheoryOfContainerIsolation in main.go.
const disableContainerEnv = "CAI_DISABLE_CONTAINER"

func maybeRunInContainer() {
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
		cmd.Env = append(os.Environ(), inContainerEnv+"=1")
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

// setupContainerFilesystem hardens the mount table inside the container
// namespace. It makes all mounts private (preventing propagation to the
// host), remounts /proc to show only namespace-local processes, makes
// /sys read-only, and masks sensitive /proc paths that could leak kernel
// information. See TheoryOfContainerIsolation in main.go.
func setupContainerFilesystem() error {
	// Make all mounts private to this namespace. This is critical: without
	// it, subsequent mount operations could propagate to the host mount
	// table via shared subtrees, modifying the host's filesystem view.
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}

	// Remount /proc to show only processes in this PID namespace.
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

	// Make /sys read-only to prevent sysfs tampering. Failure is
	// non-critical: /sys may not be a separate mount point on all systems.
	syscall.Mount("", "/sys", "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")

	// Mask sensitive /proc files that could leak kernel memory or
	// configuration. Bind-mount /dev/null over each path so reads return
	// empty content instead of privileged data. Failure for individual
	// paths is non-critical (the path may not exist on all kernels).
	for _, p := range []string{
		"/proc/kcore",
		"/proc/kallsyms",
		"/proc/bus",
		"/proc/config.gz",
	} {
		syscall.Mount("/dev/null", p, "", syscall.MS_BIND, "")
	}

	return nil
}
