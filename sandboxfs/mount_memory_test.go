package sandboxfs

import (
	"strings"
	"testing"
)

// TestValidateMountOptionsMemoryModeDoesNotRequireDBPath protects
// TheoryOfUpperLayer's mount wiring: the memory backend names no database of
// its own, so the mount does not require one. The test calls the option
// validation rather than the mount, because a real mount binds the kernel
// filesystems into the mountpoint and a test's temporary tree could not remove
// them.
func TestValidateMountOptionsMemoryModeDoesNotRequireDBPath(t *testing.T) {
	err := validateMountOptions(MountOptions{
		Mountpoint: "/tmp/mnt",
	})
	if err != nil {
		t.Fatalf("the memory layer rejected a mount without a record: %v", err)
	}
}

// TestValidateMountOptionsRequiresMountpoint protects the shared required
// field: both backends mount at a named directory, so a mount without one
// fails before any kernel call.
func TestValidateMountOptionsRequiresMountpoint(t *testing.T) {
	err := validateMountOptions(MountOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty mountpoint") {
		t.Fatalf("memory mode accepted an empty mountpoint: err = %v", err)
	}
}

// TestOpenMountStoreDefaultsToMemory protects TheoryOfUpperLayer's mount
// wiring: a mount that names no record gets the memory upper layer — this
// API's own default — and a mount that names one gets the durable SQL layer.
// The test calls the store selection rather than the mount, because a real
// mount binds the kernel filesystems into the mountpoint and a test's
// temporary tree could not remove them.
func TestOpenMountStoreDefaultsToMemory(t *testing.T) {
	base := t.TempDir()
	store, absDB, err := openMountStore(MountOptions{}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if absDB != "" || store.dbPath != "" {
		t.Fatalf("the default layer records itself: path %q, abs %q", store.dbPath, absDB)
	}

	durable, absDB, err := openMountStore(MountOptions{DBPath: t.TempDir() + "/rec.db"}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer durable.Close()
	if absDB == "" || durable.dbPath == "" {
		t.Fatalf("a named record did not open the durable layer: path %q, abs %q", durable.dbPath, absDB)
	}
}
