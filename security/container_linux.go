//go:build linux

package security

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const TheoryOfContainerIsolation = `
The process runs inside a user and mount namespace whose root is read-only
except for one writable set: the current working directory, the Go toolchain
directories (GOCACHE, GOMODCACHE, GOPATH/pkg), the user config directory,
/tmp, and /dev/shm. Read access stays unrestricted, so the model can read
anything it is pointed at; only writes are confined, which is what makes an
unsupervised run safe to leave alone. TheoryOfWritableDirs owns the set's
resolution and the caching rules, and TheoryOfLandlock owns the second
enforcement plane over the same set.

The re-exec is the mechanism. main's first statement calls
MaybeRunInContainer: the parent starts a child with CLONE_NEWUSER and
CLONE_NEWNS, maps the child's root to the caller's own uid and gid — so no
privilege is gained over the host — and exits with the child's status.
containerEnv marks the second entry, where the same function applies the
hardening instead of starting another child; disableContainerEnv skips the
layer for debugging.

Every mount is made private first, so nothing the child mounts propagates
back to the host namespace, and the root is bound onto itself so the
read-only remount names a mount object this namespace owns. Making the root
read-only is the hardening. Making a directory writable again is a bind
mount of that directory onto itself plus a remount of that same bind
(mountWritableBind), and the two calls are not interchangeable: a bare
MS_REMOUNT re-mounts the underlying superblock — the read-only root — instead
of the bind, so the directory keeps the read-only state. The failure is
silent from the model's point of view, because the change blocks still apply
to the sandbox store and the store still serves every read back; only the
write to the host is missing. mountWritableBind therefore issues bind then
MS_REMOUNT|MS_BIND and returns its error, never swallowing it. See
pipeline.TheoryOfLoopChangeApply for what the missing write costs the run.

The pre-existing submounts are remounted read-only too, except the kernel
filesystems (/dev, /proc, /sys) and the writable set: a remount the
namespace does not own fails, and the read-only root already bounds that
mount, so such a failure stays a warning-free skip instead of failing the
setup.

/tmp and /dev/shm are fresh tmpfs mounts rather than bind mounts: their host
content is machine state no review can use, and a tmpfs keeps them writable
without widening the host view. Their size limit comes from the environment,
so a session that needs a large scratch space names it.

The toolchain and config directories are resolved before the child starts and
carried in goWritableDirsEnv and configDirEnv, because the go command may not
start once the mounts are hardened. The child never re-resolves them, so the
mount layer, the writable set, the sandbox store's write policy, and the
Landlock ruleset all read the same carried values and agree by construction.

The layer is best-effort: a kernel without unprivileged user namespaces, a
refused mount, or a failed re-exec prints a warning and leaves the process on
the mount policy it inherited, which is what a run outside a container gets.
A successful hardening is followed by the sandbox filesystem (see
TheoryOfSandboxRoot) and then by the Landlock ruleset, in that order: the
sandbox mount needs to open /dev/fuse and to create its record, which the
Landlock rule set does not allow the process to do afterwards.
`

// disableContainerEnv skips the container layer, leaving the process on the
// mount policy it inherited. See TheoryOfContainerIsolation.
const disableContainerEnv = "CAI_DISABLE_CONTAINER"

// containerEnv marks the re-executed child: the child applies the hardening
// instead of starting another child. See TheoryOfContainerIsolation.
const containerEnv = "CAI_IN_CONTAINER"

// goWritableDirsEnv carries the Go toolchain directories resolved before the
// mount restrictions, one path per os.PathListSeparator. See
// TheoryOfContainerIsolation and TheoryOfWritableDirs.
const goWritableDirsEnv = "CAI_GO_WRITABLE_DIRS"

// configDirEnv carries the user config directory resolved before the mount
// restrictions. See TheoryOfContainerIsolation and TheoryOfWritableDirs.
const configDirEnv = "CAI_CONFIG_DIR"

// tmpfsSizeEnv names the size limit of the sandbox's tmpfs mounts; unset
// leaves the kernel default. See TheoryOfContainerIsolation.
const tmpfsSizeEnv = "CAI_TMPFS_SIZE"

// kernelFilesystemMounts lists the mount points the read-only hardening
// leaves alone: the kernel filesystems keep the writability the process needs
// for terminals, ptys, and process bookkeeping, and their content is not
// reviewable project state. See TheoryOfContainerIsolation.
var kernelFilesystemMounts = []string{"/dev", "/proc", "/sys"}

// tmpfsMounts lists the directories the layer mounts as fresh tmpfs, in the
// order they are mounted. See TheoryOfContainerIsolation.
var tmpfsMounts = []string{"/tmp", "/dev/shm"}

// MaybeRunInContainer places the process inside the container namespace and
// hardens its filesystem, or leaves it unchanged when the layer is disabled,
// already applied, or unavailable. The parent re-executes itself and exits
// with the child's status; the child hardens its own filesystem in place.
// See TheoryOfContainerIsolation.
func MaybeRunInContainer() {
	if os.Getenv(disableContainerEnv) != "" {
		return
	}
	if os.Getenv(containerEnv) != "" {
		applyContainerFilesystem()
		return
	}
	runInContainer()
}

// runInContainer re-executes the process inside a user and mount namespace and
// exits with the child's status. The toolchain and config directories are
// resolved here, before the mounts, and carried to the child through the
// environment, because the go command may not start once the mounts are
// hardened. See TheoryOfContainerIsolation.
func runInContainer() {
	exePath, err := os.Executable()
	if err != nil {
		warnContainer("resolve the executable", err)
		return
	}
	env := append(os.Environ(),
		containerEnv+"=1",
		goWritableDirsEnv+"="+strings.Join(resolveGoWritableDirs(), string(os.PathListSeparator)),
		configDirEnv+"="+resolveConfigDir(),
	)
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS,
		UidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getuid(),
			Size:        1,
		}},
		GidMappings: []syscall.SysProcIDMap{{
			ContainerID: 0,
			HostID:      os.Getgid(),
			Size:        1,
		}},
		GidMappingsEnableSetgroups: false,
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		warnContainer("start the container", err)
		return
	}
	os.Exit(0)
}

// applyContainerFilesystem hardens the child's filesystem: the read-only root
// and the writable binds, then the sandbox filesystem, then the Landlock
// ruleset. Each step is best-effort on its own, so one refused layer leaves
// the remaining layers to confine the process instead of ending the run.
// The order is load-bearing: the sandbox mount opens /dev/fuse and creates
// its record, which the Landlock ruleset does not admit afterwards. See
// TheoryOfContainerIsolation and TheoryOfSandboxRoot.
func applyContainerFilesystem() {
	cwd, err := os.Getwd()
	if err != nil {
		warnContainer("determine the working directory", err)
		return
	}
	if err := setupContainerFilesystem(cwd); err != nil {
		warnContainer("set up the container filesystem", err)
	}
	if err := setupSandboxRoot(cwd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: sandbox filesystem unavailable (%v), continuing without it\n", err)
	}
	applyLandlockFilesystemPolicy()
}

// setupContainerFilesystem hardens the process filesystem in the child of
// MaybeRunInContainer, where the user namespace supplies the mount capability:
// every mount is made private, the root is made read-only, the pre-existing
// submounts follow it, /tmp and /dev/shm become fresh tmpfs mounts, and the
// writable set is bind-remounted read-write. See TheoryOfContainerIsolation.
func setupContainerFilesystem(cwd string) error {
	if err := mountSyscall("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}
	if err := mountSyscall("/", "/", "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind the root: %w", err)
	}
	if err := mountSyscall("", "/", "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("make the root read-only: %w", err)
	}
	writable := writableMountDirs(cwd)
	hardenSubmounts(writable)
	for _, dir := range tmpfsMounts {
		if err := mountTmpfs(dir, tmpfsMountData(tmpfsSizeEnv)); err != nil {
			return err
		}
	}
	for _, dir := range writable {
		if err := mountWritableBind(dir); err != nil {
			return err
		}
	}
	setNoNewPrivs()
	return nil
}

// writableMountDirs returns the directories the mount layer makes writable
// again after the root turned read-only: the working directory, the Go
// toolchain directories and the config directory resolved before the mounts.
// /tmp and /dev/shm are absent because their fresh tmpfs mounts are writable
// by construction. See TheoryOfContainerIsolation and TheoryOfWritableDirs.
func writableMountDirs(cwd string) []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		dir = filepath.Clean(dir)
		if dir == "/" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	add(cwd)
	for _, dir := range filepath.SplitList(os.Getenv(goWritableDirsEnv)) {
		add(dir)
	}
	add(os.Getenv(configDirEnv))
	return dirs
}

// mountTmpfs mounts a fresh tmpfs at dir. The mount is read-write by
// construction, so no bind-remount follows it. The directory must already
// exist: a root turned read-only cannot create it.
// See TheoryOfContainerIsolation.
func mountTmpfs(dir, data string) error {
	if err := mountSyscall("tmpfs", dir, "tmpfs",
		syscall.MS_NOSUID|syscall.MS_NODEV, data); err != nil {
		return fmt.Errorf("mount tmpfs at %s: %w", dir, err)
	}
	return nil
}

// mountSyscall issues one mount call. It is a variable so a test drives the
// flag sequence without a kernel mount. See mountWritableBind.
var mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
	return syscall.Mount(source, target, fstype, flags, data)
}

// mountWritableBind makes dir writable again after the root turned read-only:
// one bind mount of dir onto itself, then a remount of that bind carrying
// MS_REMOUNT|MS_BIND. The second call must carry MS_BIND: a bare MS_REMOUNT
// re-mounts the underlying superblock — the read-only root — instead of the
// bind, so the directory keeps the read-only state while the session still
// reads its own writes back from the sandbox store, and only the write to the
// host is missing. The error is therefore returned, never swallowed. See
// TheoryOfContainerIsolation and pipeline.TheoryOfLoopChangeApply.
func mountWritableBind(dir string) error {
	if err := mountSyscall(dir, dir, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind %s: %w", dir, err)
	}
	if err := mountSyscall("", dir, "", syscall.MS_REMOUNT|syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("remount %s writable: %w", dir, err)
	}
	return nil
}

// hardenSubmounts extends the read-only root over the pre-existing submounts:
// each mount point outside the writable set and outside the kernel
// filesystems is remounted read-only the same way the root was. A remount the
// namespace does not own fails, and the read-only root already bounds that
// mount, so the failure is a skip rather than a setup error. See
// TheoryOfContainerIsolation.
func hardenSubmounts(writable []string) {
	mounts, err := parseMountPoints()
	if err != nil {
		return
	}
	for _, mountPoint := range mounts {
		if mountPoint == "/" || keptWritable(mountPoint, writable) {
			continue
		}
		_ = mountSyscall("", mountPoint, "",
			syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, "")
	}
}

// keptWritable reports whether mountPoint is one of the writable directories,
// lies beneath one, or belongs to a kernel filesystem the process needs
// writable. See TheoryOfContainerIsolation.
func keptWritable(mountPoint string, writable []string) bool {
	for _, dir := range writable {
		if mountPoint == dir || strings.HasPrefix(mountPoint, dir+"/") {
			return true
		}
	}
	for _, dir := range kernelFilesystemMounts {
		if mountPoint == dir || strings.HasPrefix(mountPoint, dir+"/") {
			return true
		}
	}
	return false
}

// tmpfsMountData renders the mount data of a sandbox tmpfs: a size limit when
// the named environment variable is set, and no option otherwise, so the mount
// takes the kernel default. See TheoryOfContainerIsolation.
func tmpfsMountData(envName string) string {
	size := strings.TrimSpace(os.Getenv(envName))
	if size == "" {
		return ""
	}
	return "size=" + size
}

// setNoNewPrivs sets PR_SET_NO_NEW_PRIVS, so a setuid binary the model runs
// gains no privilege and the Landlock ruleset can be restricted to the
// process. Best-effort, like the rest of the layer. See
// TheoryOfContainerIsolation and TheoryOfLandlock.
func setNoNewPrivs() {
	_ = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
}

// parseMountPoints returns the mount points of the current mount namespace,
// read from /proc/self/mountinfo. The mount point is the fifth field of each
// line. See TheoryOfContainerIsolation.
func parseMountPoints() ([]string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		out = append(out, unescapeMountPath(fields[4]))
	}
	return out, nil
}

// unescapeMountPath decodes the octal escapes the kernel writes into
// /proc/self/mountinfo: a space is \040, a tab \011, a newline \012, and a
// backslash \134. A sequence that is not an octal escape stays as written.
// See TheoryOfContainerIsolation.
func unescapeMountPath(path string) string {
	if !strings.Contains(path, `\`) {
		return path
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' && i+3 < len(path) {
			value := 0
			octal := true
			for _, c := range path[i+1 : i+4] {
				if c < '0' || c > '7' {
					octal = false
					break
				}
				value = value*8 + int(c-'0')
			}
			if octal {
				b.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

// warnContainer reports a refused container layer and leaves the process on
// the mount policy it inherited. A warning, not an error: the layer is
// best-effort, and a run without it is the behavior of a host that offers no
// user namespace. See TheoryOfContainerIsolation.
func warnContainer(what string, err error) {
	fmt.Fprintf(os.Stderr, "Warning: container isolation unavailable (%s: %v), continuing without it\n", what, err)
}
