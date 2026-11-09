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

// TestOpenMountStoreDefaultsToMemory protects the mount's layer selection: an
// empty record path selects the memory layer and names no record, while a
// named record path opens the durable directory record.
func TestOpenMountStoreDefaultsToMemory(t *testing.T) {
	base := t.TempDir()
	store, abs, err := openMountStore(MountOptions{}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.recordPath != "" || abs != "" {
		t.Fatalf("memory layer named a record: %q %q", store.recordPath, abs)
	}
	durable, absDB, err := openMountStore(MountOptions{DBPath: t.TempDir()}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer durable.Close()
	if durable.recordPath == "" || absDB == "" || durable.recordPath != absDB {
		t.Fatalf("durable layer named no record: %q %q", durable.recordPath, absDB)
	}
}
