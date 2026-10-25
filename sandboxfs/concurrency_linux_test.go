//go:build linux

package sandboxfs

import (
	"path/filepath"
	"testing"
	"time"
)

// TestNodeOfStatsOutsideTheNodeTable protects TheoryOfFuseMount: a node id is
// allocated without holding the node table across the store lookup. That
// lookup reaches the store's one sqlite connection, and holding the table for
// the round trip makes every other user of the table wait for it. The stat
// therefore runs before the lock is taken, and an answer arrives while another
// holder keeps the table.
func TestNodeOfStatsOutsideTheNodeTable(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "sandbox.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	s := newFuseServer(store, -1)
	s.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.nodeOf("/missing", true)
		done <- err
	}()
	select {
	case err := <-done:
		s.mu.Unlock()
		if err == nil {
			t.Fatal("nodeOf allocated a node id for a path the store does not hold")
		}
	case <-time.After(5 * time.Second):
		s.mu.Unlock()
		<-done
		t.Fatal("nodeOf waited for the node table before it looked the path up")
	}
}
