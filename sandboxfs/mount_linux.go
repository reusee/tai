//go:build linux

package sandboxfs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// MountOptions configures one sandbox mount. See TheoryOfFuseMount.
type MountOptions struct {
	// Base is the read-through base directory. Empty means the host
	// root: every path a process reads falls through to the host.
	Base string
	// DBPath is the sqlite upper-layer database file. The mount opens it
	// before any pivot and holds one connection for the session's life,
	// so the file may sit inside the filesystem this mount serves. Its
	// directory must remain visible at the same path after the pivot: the
	// working directory is, /tmp is not, because the mount replaces /tmp
	// with a fresh tmpfs. See TheoryOfFuseMount and TheoryOfPassthrough.
	DBPath string
	// Mountpoint is the directory the filesystem mounts at. Created when
	// absent. The path is a host path, not a sandbox path.
	Mountpoint string
	// Passthrough lists host directories that bypass the store.
	// DefaultPassthrough carries the design's own set; a caller adds the
	// directories its environment alone knows. See TheoryOfPassthrough.
	Passthrough []string
	// WritePolicy confines the store's mutations; a refused path returns
	// an error wrapping os.ErrPermission. Nil admits every write.
	// See TheoryOfWritePolicy.
	WritePolicy func(string) error
	// Pivot, when true, pivots the process root to the mountpoint after
	// mounting, so the sandbox takes over every path the process and its
	// children touch. The store's base handle is opened before the
	// pivot, so the server keeps reading the host after the pivot.
	Pivot bool
}

// DefaultPassthrough returns the passthrough directories the design names
// as valueless for review: the kernel filesystems, /tmp, the Go build and
// module caches, and the user config directory. The caches are the largest
// read volume of a Go task, and none of these trees holds content a review
// can use, so leaving them behind the store charges the request path for
// traffic no review can use. See TheoryOfPassthrough.
func DefaultPassthrough() []string {
	paths := []string{"/dev", "/proc", "/sys", "/tmp"}
	paths = append(paths, goCacheDirs()...)
	if dir, err := os.UserConfigDir(); err == nil && filepath.IsAbs(dir) {
		paths = append(paths, filepath.Clean(dir))
	}
	return paths
}

// goCacheDirs names the Go build and module caches of the current
// environment. Each variable is resolved on its own, so an environment that
// exports one keeps the other's standard location. A path the environment
// does not name falls back to the layout the go command uses: the build cache
// under the user cache directory and the module cache under the home
// directory's GOPATH. A relative path — the go command's "off" for GOCACHE —
// is skipped, and a path that does not exist is skipped by the mount.
// See TheoryOfPassthrough.
func goCacheDirs() []string {
	var out []string
	add := func(p string) {
		if p == "" || !filepath.IsAbs(p) {
			return
		}
		out = append(out, filepath.Clean(p))
	}
	gopaths := filepath.SplitList(os.Getenv("GOPATH"))
	if len(gopaths) == 0 {
		if dir, err := os.UserHomeDir(); err == nil {
			gopaths = []string{filepath.Join(dir, "go")}
		}
	}
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			cache = filepath.Join(dir, "go-build")
		}
	}
	add(cache)
	add(os.Getenv("GOMODCACHE"))
	for _, gp := range gopaths {
		add(filepath.Join(gp, "pkg"))
	}
	return out
}

// recursivePassthrough lists the host trees whose submounts must come with
// them: /dev carries the pty and shared-memory mounts, /proc and /sys carry
// kernel-managed submounts. Every other passthrough binds one directory.
var recursivePassthrough = map[string]bool{
	"/dev":  true,
	"/proc": true,
	"/sys":  true,
}

// MountedFS is one mounted sandbox filesystem.
type MountedFS struct {
	// Store is the sqlite-backed upper layer serving the mount.
	Store *Store
	// Mountpoint is the directory the filesystem mounted at, as the host
	// sees it before a pivot.
	Mountpoint string

	devFd  int
	server *fuseServer
	// stopTiming stops the periodic operation-timing reporter; nil when
	// the timing environment left reporting disabled. Unmount stops it
	// and writes the final snapshot, so a session that ends before the
	// first report interval still reports where its time went.
	// See TheoryOfOperationTiming.
	stopTiming func()
}

// Mount mounts the sandbox filesystem and, when requested, pivots the
// process root to it. See TheoryOfFuseMount and TheoryOfPassthrough.
func Mount(opts MountOptions) (*MountedFS, error) {
	if opts.Mountpoint == "" {
		return nil, fmt.Errorf("sandboxfs: empty mountpoint")
	}
	if opts.DBPath == "" {
		return nil, fmt.Errorf("sandboxfs: empty database path")
	}
	if opts.Base == "" {
		opts.Base = "/"
	}
	absBase, err := filepath.Abs(opts.Base)
	if err != nil {
		return nil, err
	}
	absDB, err := filepath.Abs(opts.DBPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.Mountpoint, 0o755); err != nil {
		return nil, err
	}
	// Clone the base into a private bind view made before any FUSE mount,
	// so the store's path-based reads never cross the mount it serves.
	// The view is detached before this function returns: the store's
	// base handle keeps it alive. See TheoryOfFuseMount.
	viewDir, detachView, err := privateBaseView(absBase)
	if err != nil {
		return nil, err
	}
	store, err := OpenWithPolicy(absDB, viewDir, opts.WritePolicy)
	if err != nil {
		detachView()
		return nil, err
	}
	// The store's base is the private view, not the host base the database
	// was opened with. A database inside the base reaches the sandbox as the
	// path relative to the base, so the two names are recorded by identity
	// through the view; without this the store would compare the sandbox path
	// with a host path they do not share and miss the match.
	// See TheoryOfSandboxFilesystem.
	if rel, relErr := filepath.Rel(absBase, absDB); relErr == nil &&
		rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		store.dbPath = filepath.Join(viewDir, rel)
	}
	detachView()
	devFd, err := unix.Open("/dev/fuse", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("sandboxfs: open /dev/fuse: %w", err)
	}
	data := fmt.Sprintf("fd=%d,rootmode=%o,user_id=%d,group_id=%d",
		devFd, syscall.S_IFDIR|0o755, os.Getuid(), os.Getgid())
	if err := unix.Mount("sandboxfs", opts.Mountpoint, "fuse",
		unix.MS_NOSUID|unix.MS_NODEV, data); err != nil {
		_ = unix.Close(devFd)
		_ = store.Close()
		return nil, fmt.Errorf("sandboxfs: mount: %w", err)
	}
	server := newFuseServer(store, devFd)
	mfs := &MountedFS{
		Store:      store,
		Mountpoint: opts.Mountpoint,
		devFd:      devFd,
		server:     server,
	}
	go server.run()
	if timing := os.Getenv(timingEnv); timing != "" {
		// The reporter runs until Unmount stops it: a measurement is a
		// debugging aid, and the final snapshot at unmount covers a session
		// too short to reach the first interval.
		// See TheoryOfOperationTiming.
		mfs.stopTiming = server.timings.StartReporter(timingIntervalOf(timing), os.Stderr)
	}
	if err := mfs.bindPassthrough(opts.Passthrough); err != nil {
		_ = mfs.Unmount()
		return nil, err
	}
	if opts.Pivot {
		if err := pivotRoot(opts.Mountpoint); err != nil {
			_ = mfs.Unmount()
			return nil, err
		}
	}
	return mfs, nil
}

// Timings returns the mount's operation-timing aggregate, or nil when the
// mount carries no server. A measurement reads it after a workload ran.
// See TheoryOfOperationTiming.
func (m *MountedFS) Timings() *OpTimings {
	if m == nil || m.server == nil {
		return nil
	}
	return m.server.timings
}

// privateBaseView clones base into a fresh bind mount that predates the FUSE
// mount the store will serve, and returns the private path with a detach
// function. Without the clone the store's path-based reads would recurse:
// the daemon answers LOOKUP for a path, the handler reads the base by path,
// and the read crosses the mount it is answering on. The clone is made
// before any FUSE mount, so it holds no reference to the mountpoint.
// Cleanup is lazy: MNT_DETACH hides the clone from the namespace while the
// store's base handle keeps it alive. See TheoryOfFuseMount.
func privateBaseView(base string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "tai-base-view-")
	if err != nil {
		return "", nil, err
	}
	if err := unix.Mount(base, dir, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		_ = os.Remove(dir)
		return "", nil, fmt.Errorf("sandboxfs: bind base view: %w", err)
	}
	detach := func() {
		_ = unix.Unmount(dir, unix.MNT_DETACH)
		_ = os.Remove(dir)
	}
	return dir, detach, nil
}

func (m *MountedFS) bindPassthrough(paths []string) error {
	seen := make(map[string]bool)
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if p == "/" || seen[p] {
			continue
		}
		seen[p] = true
		target := filepath.Join(m.Mountpoint, p)
		if p == "/tmp" {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("sandboxfs: create passthrough %s: %w", p, err)
			}
			if err := unix.Mount("tmpfs", target, "tmpfs",
				unix.MS_NOSUID|unix.MS_NODEV, ""); err != nil {
				return fmt.Errorf("sandboxfs: mount /tmp tmpfs: %w", err)
			}
			continue
		}
		// The source is resolved before its target is created: the mount is
		// best-effort, and a directory created for a mount that never happens
		// would enter the store as a change the session never made.
		// See TheoryOfPassthrough.
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("sandboxfs: create passthrough %s: %w", p, err)
		}
		flags := uintptr(unix.MS_BIND)
		if recursivePassthrough[p] {
			flags |= unix.MS_REC
		}
		if err := unix.Mount(p, target, "", flags, ""); err != nil {
			return fmt.Errorf("sandboxfs: bind passthrough %s: %w", p, err)
		}
	}
	return nil
}

// pivotRoot makes the mountpoint the process root from the current
// directory. The mountpoint must be a mount point. The old root is stacked
// over the new one by pivot_root and detached, so the new root replaces it
// everywhere and no clone of the mount remains in the detached tree. The
// working directory is restored after the pivot, because the caller's
// relative paths must keep resolving.
func pivotRoot(newRoot string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(newRoot)
	if err != nil {
		return err
	}
	if err := os.Chdir(abs); err != nil {
		return fmt.Errorf("sandboxfs: enter mountpoint: %w", err)
	}
	if err := unix.PivotRoot(".", "."); err != nil {
		_ = os.Chdir(cwd)
		return fmt.Errorf("sandboxfs: pivot_root: %w", err)
	}
	if err := unix.Unmount(".", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("sandboxfs: detach old root: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return err
	}
	if cwd != "" && cwd != "/" && cwd != abs {
		if err := os.Chdir(cwd); err != nil {
			return fmt.Errorf("sandboxfs: restore working directory: %w", err)
		}
	}
	return nil
}

// Unmount detaches the mount and stops the FUSE server. It is best-effort:
// a mount with open files may linger until the last holder closes, and
// after a pivot the mountpoint path belongs to the detached tree.
func (m *MountedFS) Unmount() error {
	var err error
	if m.Mountpoint != "" {
		if e := unix.Unmount(m.Mountpoint, unix.MNT_DETACH); e != nil {
			err = e
		}
	}
	if m.devFd >= 0 {
		_ = unix.Close(m.devFd)
		m.devFd = -1
	}
	if m.server != nil {
		<-m.server.done
		// The final snapshot is the whole session's distribution, written
		// after the server stopped so no request lands beside it.
		// See TheoryOfOperationTiming.
		m.reportTimings(os.Stderr)
	}
	if m.Store != nil {
		if e := m.Store.Close(); err == nil {
			err = e
		}
	}
	return err
}

// reportTimings stops the periodic operation-timing reporter and writes the
// final snapshot to w, so a session that ends before the first report
// interval still reports where its time went. Reporting stays gated: a mount
// without the timing environment carries no stop function and writes nothing,
// and a second call writes nothing because the first cleared it.
// See TheoryOfOperationTiming.
func (m *MountedFS) reportTimings(w io.Writer) {
	if m == nil || m.server == nil || m.server.timings == nil || m.stopTiming == nil {
		return
	}
	m.stopTiming()
	m.stopTiming = nil
	if w != nil {
		_, _ = io.WriteString(w, m.server.timings.Report())
	}
}
