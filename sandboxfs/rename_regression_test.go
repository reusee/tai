package sandboxfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type failPathRowOnce struct {
	upperStore
	path string
	err  error
	done bool
}

func (u *failPathRowOnce) row(p string) (fileRow, bool, error) {
	if !u.done && p == u.path {
		u.done = true
		return fileRow{}, false, u.err
	}
	return u.upperStore.row(p)
}

func TestFuseServerRenameRejectsUnknownFlags(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "old"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := newFuseServer(store, -1)
	body := encodeStruct(&fuseRename2In{
		Newdir: fuseRootID,
		Flags:  1 << 31,
	})
	body = append(body, "old\x00new\x00"...)
	_, err = server.handleRename(fuseRootID, body, true)
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("unknown RENAME2 flags: got %v, want EINVAL", err)
	}
	if _, err := store.Stat("/old"); err != nil {
		t.Fatalf("the rejected rename changed the source: %v", err)
	}
}

func TestFuseServerRenamePropagatesDestinationErrors(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "old"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	injected := errors.New("injected destination lookup failure")
	store.upper = &failPathRowOnce{
		upperStore: store.upper,
		path:       "/new",
		err:        injected,
	}
	server := newFuseServer(store, -1)
	body := encodeStruct(&fuseRename2In{
		Newdir: fuseRootID,
		Flags:  1,
	})
	body = append(body, "old\x00new\x00"...)
	_, err = server.handleRename(fuseRootID, body, true)
	if !errors.Is(err, injected) {
		t.Fatalf("destination lookup: got %v, want %v", err, injected)
	}
	if _, err := store.Stat("/old"); err != nil {
		t.Fatalf("the failed lookup changed the source: %v", err)
	}
}
