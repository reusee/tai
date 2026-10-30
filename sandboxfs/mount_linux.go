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
	// DBPath names the durable upper layer's record: a database file —
	// sqlite's, or DuckDB's when DuckDB is set — or, when DirRecord is set,
	// a directory. An empty DBPath selects the memory upper layer, which is
	// this API's own default: the session's mutations live in process
	// memory, the mount leaves no record behind, and the goal runner's
	// per-loop apply is the only path that reaches the host. The trade-off
	// is explicit: an abandoned run keeps nothing to review, because the
	// changes it never applied never left the process. The tai tool names a
	// per-session directory record instead — one directory per session,
	// inside the working directory, so every session leaves a record the
	// review command browses later. A durable record is opened before any
	// pivot and held for the session's life, so the file or directory may
	// sit inside the filesystem this mount serves; its place must remain
	// visible at the same path after the pivot: the working directory is,
	// /tmp is not, because the mount replaces /tmp with a fresh tmpfs. See
	// TheoryOfFuseMount, TheoryOfPassthrough, TheoryOfUpperLayer,
	// TheoryOfDirRecord and TheoryOfSqlDialects.
	DBPath string
	// DuckDB selects the DuckDB dialect of the durable SQL upper layer
	// instead of sqlite's. The dialect names no record of its own, so
	// DBPath must be set too; the binary must import a DuckDB driver, and
	// none is linked here, so a binary without one reports the open error
	// and names the dialect. See TheoryOfSqlDialects.
	DuckDB bool
	// DirRecord selects the directory upper layer: DBPath names a record
	// directory of ordinary files instead of a database, so the session
	// needs no driver and no schema. The directory is created when absent
	// and opened once before any pivot, so its files stay reachable through
	// the handle after the process root moves. It is exclusive with DuckDB,
	// because a directory record has no dialect. See TheoryOfDirRecord.
	DirRecord bool
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

// kernelFilesystems lists the host trees that must stay real kernel
// filesystems inside the mount. The theory names them as always passthrough,
// so the mount binds them whatever the caller passes: a caller that forgets
// /dev hands the store a character device, the store reports it as a regular
// file, and a capture of it opens the device to hash its content — a terminal
// blocks until a key arrives, and the read consumes what it waited for.
// See TheoryOfPassthrough.
var kernelFilesystems = []string{"/dev", "/proc", "/sys"}

// passthroughPaths returns the directories the mount binds over the store:
// the kernel filesystems first, then the caller's own list. The bind cleans
// the paths and skips a duplicate, so a caller that passes one of the kernel
// filesystems binds it once. See TheoryOfPassthrough.
func passthroughPaths(caller []string) []string {
	out := make([]string, 0, len(kernelFilesystems)+len(caller))
	out = append(out, kernelFilesystems...)
	return append(out, caller...)
}

// MountedFS is one mounted sandbox filesystem.
type MountedFS struct {
	// Store is the upper layer serving the mount: process memory when the
	// mount named no record — the API's own default — otherwise the durable
	// layer the mount selected at the record path: the directory record when
	// DirRecord is set, or the SQL record in sqlite's dialect or DuckDB's.
	// See TheoryOfUpperLayer, TheoryOfDirRecord and TheoryOfSqlDialects.
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

// validateMountOptions checks the mount's required fields before any kernel
// call. It exists so a test can exercise the options the upper layers require
// without mounting: a real mount binds the kernel filesystems into the
// mountpoint, and a test that mounted would leave directories its temporary
// tree cannot remove. A selected layer that names no record — a dialect or a
// record directory without DBPath — selected a layer it did not open, and the
// dialect and the directory marker are exclusive, because a directory record
// has no dialect. See TheoryOfUpperLayer, TheoryOfDirRecord and
// TheoryOfSqlDialects.
func validateMountOptions(opts MountOptions) error {
	if opts.Mountpoint == "" {
		return fmt.Errorf("sandboxfs: empty mountpoint")
	}
	if opts.DuckDB && opts.DirRecord {
		// A directory record has no dialect, so the two markers name
		// different layers; a caller that sets both selected two.
		// See TheoryOfDirRecord and TheoryOfSqlDialects.
		return fmt.Errorf("sandboxfs: DuckDB and DirRecord are exclusive; a directory record has no dialect")
	}
	if (opts.DuckDB || opts.DirRecord) && opts.DBPath == "" {
		// The dialect and the directory marker name no record of their own, so
		// a caller that selected one without naming a record selected a layer it
		// did not open. See TheoryOfDirRecord and TheoryOfSqlDialects.
		return fmt.Errorf("sandboxfs: empty database path")
	}
	return nil
}

// Mount mounts the sandbox filesystem and, when requested, pivots the process
// root to it. The upper layer behind it is process memory when DBPath names no
// record — this API's own default — and the durable layer of the selected kind
// otherwise: the directory record when DirRecord is set, sqlite's dialect or
// DuckDB's when it names a database file. The tai tool names a per-session
// directory record instead, so its sessions leave a record the sandbox command
// reviews. See TheoryOfFuseMount, TheoryOfPassthrough, TheoryOfUpperLayer,
// TheoryOfDirRecord and TheoryOfSqlDialects.
func Mount(opts MountOptions) (*MountedFS, error) {
	if err := validateMountOptions(opts); err != nil {
		return nil, err
	}
	if opts.Base == "" {
		opts.Base = "/"
	}
	absBase, err := filepath.Abs(opts.Base)
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
	store, absDB, err := openMountStore(opts, viewDir)
	if err != nil {
		detachView()
		return nil, err
	}
	if absDB != "" {
		// The store's base is the private view, not the host base the
		// record was opened with. A record inside the base reaches the
		// sandbox as the path relative to the base, so the two names are
		// recorded by identity through the view; without this the store
		// would compare the sandbox path with a host path they do not share
		// and miss the match. A directory record makes the match structural:
		// every file beneath the record's own name is the record.
		// See TheoryOfSandboxFilesystem and TheoryOfDirRecord.
		if rel, relErr := filepath.Rel(absBase, absDB); relErr == nil &&
			rel != "." && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			store.dbPath = filepath.Join(viewDir, rel)
		}
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
	if err := mfs.bindPassthrough(passthroughPaths(opts.Passthrough)); err != nil {
		_ = mfs.Unmount()
		return nil, err
	}
	if err := mfs.activate(opts.Pivot, pivotRoot); err != nil {
		_ = mfs.Unmount()
		return nil, err
	}
	return mfs, nil
}

// activate makes the mount the process root when the caller asked for a pivot,
// and records the store that serves it. The pivot is a parameter so a test can
// drive the step without moving the process root, which no test can reach its
// own files through again. The record is set only after a pivot that succeeded,
// so a process whose pivot failed carries no store and every consumer degrades
// to "no sandbox" without a flag. See TheoryOfActiveStore and TheoryOfFuseMount.
func (m *MountedFS) activate(pivot bool, pivotFn func(string) error) error {
	if !pivot {
		return nil
	}
	if err := pivotFn(m.Mountpoint); err != nil {
		return err
	}
	SetActiveStore(m.Store)
	return nil
}

// openMountStore opens the upper layer the mount selects: process memory when
// DBPath names no record — the API's own default, which names nothing to
// record — otherwise the durable layer of the selected kind at the record path
// it returns: the directory record when DirRecord is set, sqlite's dialect, or
// DuckDB's when DuckDB is set. The path is empty for the memory layer, which
// records itself in no file. See TheoryOfUpperLayer, TheoryOfDirRecord and
// TheoryOfSqlDialects.
func openMountStore(opts MountOptions, base string) (store *Store, absDB string, err error) {
	if opts.DBPath == "" {
		// The API's own ephemeral default: the session's mutations live in
		// process memory, so the mount leaves no record and the goal runner's
		// per-loop apply is the only path that reaches the host. The tai tool
		// names a per-session directory record instead.
		// See TheoryOfUpperLayer.
		store, err = OpenMemoryWithPolicy(base, opts.WritePolicy)
		return store, "", err
	}
	absDB, err = filepath.Abs(opts.DBPath)
	if err != nil {
		return nil, "", err
	}
	switch {
	case opts.DirRecord:
		store, err = OpenDirWithPolicy(absDB, base, opts.WritePolicy)
	case opts.DuckDB:
		store, err = OpenDuckDBWithPolicy(absDB, base, opts.WritePolicy)
	default:
		store, err = OpenWithPolicy(absDB, base, opts.WritePolicy)
	}
	return store, absDB, err
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
