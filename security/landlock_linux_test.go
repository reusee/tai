//go:build linux

package security

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLandlockHandledAccessesABI1WriteRights(t *testing.T) {
	want := landlockAccessFSWriteFile |
		landlockAccessFSRemoveDir |
		landlockAccessFSRemoveFile |
		landlockAccessFSMakeChar |
		landlockAccessFSMakeDir |
		landlockAccessFSMakeReg |
		landlockAccessFSMakeSock |
		landlockAccessFSMakeFifo |
		landlockAccessFSMakeBlock |
		landlockAccessFSMakeSym
	if got := landlockHandledAccesses(1); got != want {
		t.Fatalf("ABI 1 handled = %#x, want %#x", got, want)
	}
}

func TestLandlockHandledAccessesGrowsWithABI(t *testing.T) {
	if got := landlockHandledAccesses(2); got&landlockAccessFSRefer == 0 {
		t.Fatal("ABI 2 must handle REFER")
	}
	if got := landlockHandledAccesses(2); got&landlockAccessFSTruncate != 0 {
		t.Fatal("ABI 2 must not handle TRUNCATE")
	}
	if got := landlockHandledAccesses(3); got&landlockAccessFSTruncate == 0 {
		t.Fatal("ABI 3 must handle TRUNCATE")
	}
	if landlockHandledAccesses(7) != landlockHandledAccesses(3) {
		t.Fatal("no further filesystem rights are handled beyond ABI 3")
	}
}

func TestLandlockHandledAccessesExcludesReadExecuteIoctl(t *testing.T) {
	excluded := map[string]uint64{
		"EXECUTE":   landlockAccessFSExecute,
		"READ_FILE": landlockAccessFSReadFile,
		"READ_DIR":  landlockAccessFSReadDir,
		"IOCTL_DEV": landlockAccessFSIoctlDev,
	}
	for abi := 1; abi <= 7; abi++ {
		handled := landlockHandledAccesses(abi)
		for name, bit := range excluded {
			if handled&bit != 0 {
				t.Fatalf("ABI %d must not handle %s", abi, name)
			}
		}
	}
}

func TestLandlockWritableDirsMirrorsMountWritableSet(t *testing.T) {
	t.Setenv(goWritableDirsEnv, strings.Join([]string{
		"/root/.cache/go-build",
		"/root/go/pkg/mod",
		"/home/user/proj/nested",
	}, string(filepath.ListSeparator)))
	t.Setenv(configDirEnv, "/root/.config/tai")

	dirs := landlockWritableDirs("/home/user/proj")

	for _, want := range []string{
		"/root/.cache/go-build",
		"/root/go/pkg/mod",
		"/root/.config/tai",
		"/tmp",
		"/dev/shm",
		"/home/user/proj",
	} {
		if !slices.Contains(dirs, want) {
			t.Errorf("missing writable dir %s in %v", want, dirs)
		}
	}
	if slices.Contains(dirs, "/home/user/proj/nested") {
		t.Error("directories under the working directory are covered by its rule")
	}
}

func TestApplyLandlockFilesystemPolicyDisabledByEnv(t *testing.T) {
	t.Setenv(disableLandlockEnv, "1")
	// Returns before any syscall; the test process must not be restricted.
	applyLandlockFilesystemPolicy()
}
