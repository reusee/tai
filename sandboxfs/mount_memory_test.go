package sandboxfs

import (
	"strings"
	"testing"
)

// TestValidateMountOptionsMemoryModeDoesNotRequireDBPath protects
// TheoryOfUpperLayer's mount wiring: the memory backend names no database,
// so the mount does not require one. The test calls the option validation
// rather than the mount, because a real mount binds the kernel filesystems
// into the mountpoint and a test's temporary tree could not remove them.
func TestValidateMountOptionsMemoryModeDoesNotRequireDBPath(t *testing.T) {
	err := validateMountOptions(MountOptions{
		Memory:     true,
		Mountpoint: "/tmp/mnt",
	})
	if err != nil {
		t.Fatalf("memory mode rejected an empty database path: %v", err)
	}
}

// TestValidateMountOptionsSQLiteModeRequiresDBPath protects the sqlite
// default: a mount that selects the sqlite upper layer must name its
// database, because the record's path is the caller's declaration of where
// the session lives. See TheoryOfUpperLayer and TheoryOfSqliteUpper.
func TestValidateMountOptionsSQLiteModeRequiresDBPath(t *testing.T) {
	err := validateMountOptions(MountOptions{Mountpoint: "/tmp/mnt"})
	if err == nil || !strings.Contains(err.Error(), "empty database path") {
		t.Fatalf("sqlite mode accepted an empty database path: err = %v", err)
	}
}

// TestValidateMountOptionsRequiresMountpoint protects the shared required
// field: both backends mount at a named directory, so a mount without one
// fails before any kernel call.
func TestValidateMountOptionsRequiresMountpoint(t *testing.T) {
	err := validateMountOptions(MountOptions{Memory: true})
	if err == nil || !strings.Contains(err.Error(), "empty mountpoint") {
		t.Fatalf("memory mode accepted an empty mountpoint: err = %v", err)
	}
}
