//go:build linux

package security

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
read-only remount names a mount object this namespace owns. The writable
set becomes independent writable mounts before the root turns read-only: a
failure at that stage aborts the setup while the process still carries its
inherited policy, so the best-effort fallback keeps the working directory
writable, and the read-only root, being the last step that can fail, cannot
take write access away from those mounts because mount flags are
per-mount. Making the root read-only is the hardening.

The writable set enters the layer as the process reports it, while the
kernel names mount points by their resolved path. A symlinked component
therefore makes the hardening pass read the writable bind at the working
directory as a submount outside the set, remount that bind read-only, and
leave the working directory unwritable under an otherwise correct policy.
Every entry is canonicalized before use (canonicalPath), so the mount layer,
the hardening comparison, the pre-read-only verification, and the Landlock
rules name one path.

Making a directory writable is a bind mount of that directory onto itself
plus a remount of that same bind (mountWritableBind), and the two calls are
not interchangeable: a bare MS_REMOUNT re-mounts the underlying superblock —
the read-only root — instead of the bind, so the directory keeps the
read-only state and every write meant for it fails. mountWritableBind
therefore issues bind then MS_REMOUNT|MS_BIND and returns its error, never
swallowing it, so the setup reports the fault instead of leaving a
directory silently read-only.

The pre-existing submounts are remounted read-only too, except the kernel
filesystems (/dev, /proc, /sys) and the writable set: a remount the
namespace does not own fails, and the read-only root already bounds that
mount, so such a failure stays a warning-free skip instead of failing the
setup. This hardening runs before the fresh tmpfs mounts, because it
enumerates the namespace's mounts: a tmpfs mounted first would be seen as
a pre-existing submount and lose the writability it was created for.

/tmp and /dev/shm are fresh tmpfs mounts rather than bind mounts: their host
content is machine state no review can use, and a tmpfs keeps them writable
without widening the host view. Their size limit comes from the environment,
so a session that needs a large scratch space names it. A tmpfs mount is
skipped when its mount point contains a writable directory, the working
directory included: replacing it would hide a path the session must write.

The toolchain and config directories are resolved before the child starts and
carried in goWritableDirsEnv and configDirEnv, because the go command may not
start once the mounts are hardened. The child never re-resolves them, so the
mount layer, the writable set, and the Landlock ruleset all read the same
carried values and agree by construction.

The layer is best-effort: a kernel without unprivileged user namespaces, a
refused mount, or a failed re-exec prints a warning and leaves the process on
the mount policy it inherited, which is what a run outside a container gets.
The read-only root runs last for that reason: no warning can repair a
read-only working directory, so no earlier step may leave one, and the setup
verifies before that step that the topmost mount at the working directory is
read-write (verifyWritableMount). A read-only result aborts the setup before
the root turns read-only, so the warning path never stacks a read-only root
over a working directory that already rejects writes; the filesystem root,
which cannot host a writable working directory under a read-only root,
aborts it too. Only a fully applied mount layer is followed by the Landlock
ruleset, because the ruleset re-enforces that policy and must never narrow
the inherited one.
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

// applyContainerFilesystem hardens the child's filesystem: the writable
// mounts, the read-only root, and the Landlock ruleset. A failed mount layer
// returns to the inherited policy here, and Landlock is applied only after a
// fully applied mount layer, because the ruleset re-enforces that policy and
// would otherwise narrow the inherited one. See TheoryOfContainerIsolation.
func applyContainerFilesystem() {
	cwd, err := os.Getwd()
	if err != nil {
		warnContainer("determine the working directory", err)
		return
	}
	cwd = canonicalPath(cwd)
	if err := setupContainerFilesystem(cwd); err != nil {
		warnContainer("set up the container filesystem", err)
		return
	}
	applyLandlockFilesystemPolicy()
}

// setupContainerFilesystem hardens the process filesystem in the child of
// MaybeRunInContainer, where the user namespace supplies the mount capability:
// every mount is made private, the writable set becomes independent writable
// mounts, the pre-existing submounts are hardened, /tmp and /dev/shm become
// fresh tmpfs mounts, the working directory's mount is confirmed read-write,
// and only then does the root turn read-only before the final no-new-privs
// flag. The order is load-bearing: a failure before the read-only root leaves
// the inherited policy intact, so the best-effort fallback keeps the working
// directory writable. See TheoryOfContainerIsolation.
func setupContainerFilesystem(cwd string) error {
	if err := mountSyscall("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make mounts private: %w", err)
	}
	if err := mountSyscall("/", "/", "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind the root: %w", err)
	}
	writable := writableMountDirs(cwd)
	for _, dir := range writable {
		if err := mountWritableBind(dir); err != nil {
			return err
		}
	}
	hardenSubmounts(writable)
	for _, dir := range tmpfsMounts {
		if hidesWritable(dir, writable) {
			continue
		}
		if err := mountTmpfs(dir, tmpfsMountData(tmpfsSizeEnv)); err != nil {
			return err
		}
	}
	if err := verifyWritableMount(cwd); err != nil {
		return fmt.Errorf("the working directory would not stay writable: %w", err)
	}
	if err := mountSyscall("", "/", "", syscall.MS_REMOUNT|syscall.MS_BIND|syscall.MS_RDONLY, ""); err != nil {
		return fmt.Errorf("make the root read-only: %w", err)
	}
	setNoNewPrivs()
	return nil
}

// writableMountDirs returns the directories the mount layer makes writable
// again after the root turned read-only: the working directory, the Go
// toolchain directories and the config directory resolved before the mounts.
// Each entry is canonicalized, so the hardening pass compares it against the
// mount table by the path the kernel names. /tmp and /dev/shm are absent
// because their fresh tmpfs mounts are writable by construction.
// See TheoryOfContainerIsolation and TheoryOfWritableDirs.
func writableMountDirs(cwd string) []string {
	var dirs []string
	seen := make(map[string]bool)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		dir = canonicalPath(dir)
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

// canonicalPath resolves the symlinks of dir, so a path the layer compares
// against the kernel's mount table names the same directory the kernel names.
// Mount calls resolve symlinks themselves, so an unresolved path would make
// the layer compare one path and mount another. A path that cannot be resolved
// stays as written. See TheoryOfContainerIsolation.
func canonicalPath(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return filepath.Clean(dir)
}

// mountSyscall issues one mount call. It is a variable so a test drives the
// flag sequence without a kernel mount. See mountWritableBind.
var mountSyscall = func(source, target, fstype string, flags uintptr, data string) error {
	return syscall.Mount(source, target, fstype, flags, data)
}

// mountWritableBind makes dir an independent writable mount: one bind mount
// of dir onto itself, then a remount of that bind carrying
// MS_REMOUNT|MS_BIND. The bind copies the source mount's flags, so the
// remount is what clears a read-only state inherited from the source. The
// second call must carry MS_BIND: a bare MS_REMOUNT re-mounts the underlying
// superblock instead of the bind, so the directory keeps the read-only state
// and every write meant for it fails. The error is therefore returned, never
// swallowed, so the setup reports the fault instead of leaving a directory
// silently read-only. See TheoryOfContainerIsolation.
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
// lies beneath one, is a tmpfs mount point the layer fills with a fresh
// writable filesystem, or belongs to a kernel filesystem the process needs
// writable. A tmpfs mount point stays writable in both outcomes: the fresh
// mount replaces it, and a skipped mount leaves the filesystem the writable
// set promises. See TheoryOfContainerIsolation and TheoryOfWritableDirs.
func keptWritable(mountPoint string, writable []string) bool {
	if beneathAny(mountPoint, writable) {
		return true
	}
	if beneathAny(mountPoint, tmpfsMounts) {
		return true
	}
	return beneathAny(mountPoint, kernelFilesystemMounts)
}

// beneathAny reports whether path is one of dirs or lies beneath one of them.
// See TheoryOfContainerIsolation.
func beneathAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

// hidesWritable reports whether mounting over dir would hide a writable
// directory, the working directory included. A hidden writable directory
// makes every write meant for it resolve elsewhere, so the tmpfs mount is
// skipped instead. See TheoryOfContainerIsolation.
func hidesWritable(dir string, writable []string) bool {
	for _, w := range writable {
		if w == dir || strings.HasPrefix(w, dir+"/") {
			return true
		}
	}
	return false
}

// writableMountError reports why the working directory would not stay
// writable, or nil when its topmost mount is read-write. The greatest mount
// ID at the path is the topmost mount, which is the one a write resolves
// through. The filesystem root is unwritable by construction, because the last
// step turns the root mount read-only, and a directory that no mount names has
// no writable mount to write through. See TheoryOfContainerIsolation.
func writableMountError(entries []mountEntry, dir string) error {
	if dir == "/" {
		return errors.New("the filesystem root cannot stay writable under a read-only root")
	}
	var top mountEntry
	found := false
	for _, entry := range entries {
		if entry.mountPoint != dir {
			continue
		}
		if !found || entry.id > top.id {
			top = entry
			found = true
		}
	}
	if !found {
		return fmt.Errorf("no mount names %s", dir)
	}
	if top.readOnly {
		return fmt.Errorf("the mount at %s is read-only", dir)
	}
	return nil
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

// verifyWritableMount reports whether the topmost mount at dir is read-write.
// It runs before the root turns read-only, so its error aborts the setup
// instead of leaving a read-only working directory under a read-only root. It
// is a variable so a test drives the setup without the test process's own
// mount table. See writableMountError.
var verifyWritableMount = func(dir string) error {
	entries, err := parseMountEntries()
	if err != nil {
		return fmt.Errorf("read the mount table: %w", err)
	}
	return writableMountError(entries, dir)
}

// setNoNewPrivs sets PR_SET_NO_NEW_PRIVS, so a setuid binary the model runs
// gains no privilege and the Landlock ruleset can be restricted to the
// process. Best-effort, like the rest of the layer. See
// TheoryOfContainerIsolation and TheoryOfLandlock.
func setNoNewPrivs() {
	_ = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
}

// mountEntry is one line of /proc/self/mountinfo reduced to the fields the
// writable-mount check needs: the mount ID orders stacked mounts, the mount
// point names the directory, and the options say whether the mount is
// read-only. See TheoryOfContainerIsolation.
type mountEntry struct {
	id         int
	mountPoint string
	readOnly   bool
}

// parseMountEntries reads the mounts of the current mount namespace. The
// kernel writes one mount per line of /proc/self/mountinfo: the mount ID is
// the first field, the mount point the fifth, and the per-mount options the
// sixth, where "ro" marks a read-only mount.
// See TheoryOfContainerIsolation.
func parseMountEntries() ([]mountEntry, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	var out []mountEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		out = append(out, mountEntry{
			id:         id,
			mountPoint: unescapeMountPath(fields[4]),
			readOnly:   slices.Contains(strings.Split(fields[5], ","), "ro"),
		})
	}
	return out, nil
}

// parseMountPoints returns the mount points of the current mount namespace,
// read from /proc/self/mountinfo. See TheoryOfContainerIsolation.
func parseMountPoints() ([]string, error) {
	entries, err := parseMountEntries()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.mountPoint)
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
