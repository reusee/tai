package sandboxfs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newConcurrencyTestStore opens a store over an empty base directory, so a
// test starts with no upper row and no base entry.
func newConcurrencyTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenDir(filepath.Join(dir, "rec.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, base
}

// TestMaterializeContentKeepsAConcurrentWritersRow protects
// TheoryOfWriteSplice: the first write of a fresh path materialises the row,
// and another writer can create that row first. The row it created is the
// content the delta lands on; replacing it drops the bytes that writer stored.
func TestMaterializeContentKeepsAConcurrentWritersRow(t *testing.T) {
	store, _ := newConcurrencyTestStore(t)
	if _, ok, err := store.row("/x.txt"); err != nil || ok {
		t.Fatalf("row = %v, %v; want no row", ok, err)
	}
	if err := store.WriteFile("/x.txt", []byte("the other writer"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The interleaving: this writer found no live regular row before the write
	// above and materialises after it.
	if _, err := store.materializeContent("/x.txt", -1, 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := store.ReadFile("/x.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "the other writer" {
		t.Fatalf("content = %q, want the bytes the other writer stored", content)
	}
}

// TestReadFileDoesNotMixRowVersions protects the same rule at the store API:
// a whole-file read must not combine the row length observed before a
// concurrent replacement with the bytes observed after it. All three upper
// layers run the same interleaving because the store shares their union logic.
func TestReadFileDoesNotMixRowVersions(t *testing.T) {
	stores := newLayerStores(t)
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			small := []byte("a")
			large := bytes.Repeat([]byte("b"), 2*contentChunkSize)
			if err := store.WriteFile("/f", small, 0o644); err != nil {
				t.Fatal(err)
			}
			const rounds = 2000
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < rounds; i++ {
					content := small
					if i%2 != 0 {
						content = large
					}
					if err := store.WriteFile("/f", content, 0o644); err != nil {
						errs <- err
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < rounds; i++ {
					data, err := store.ReadFile("/f")
					if err != nil {
						errs <- err
						return
					}
					if !bytes.Equal(data, small) && !bytes.Equal(data, large) {
						errs <- fmt.Errorf("torn file: %d bytes with prefix %q", len(data), data[:min(len(data), 8)])
						return
					}
				}
			}()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
		})
	}
}

// TestChangesDoesNotMixRowVersions protects the review path: a change's
// Current bytes must be one committed version, because Apply writes exactly
// those bytes to the host. A stale row length combined with newer chunks can
// otherwise turn a valid session write into corrupted host content.
func TestChangesDoesNotMixRowVersions(t *testing.T) {
	stores := newLayerStores(t)
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			small := []byte("a")
			large := bytes.Repeat([]byte("b"), 2*contentChunkSize)
			if err := store.WriteFile("/f", small, 0o644); err != nil {
				t.Fatal(err)
			}
			const rounds = 2000
			start := make(chan struct{})
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < rounds; i++ {
					content := small
					if i%2 != 0 {
						content = large
					}
					if err := store.WriteFile("/f", content, 0o644); err != nil {
						errs <- err
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < rounds; i++ {
					changes, err := store.Changes()
					if err != nil {
						errs <- err
						return
					}
					if len(changes) != 1 {
						errs <- fmt.Errorf("got %d changes, want one", len(changes))
						return
					}
					data := changes[0].Current
					if !bytes.Equal(data, small) && !bytes.Equal(data, large) {
						errs <- fmt.Errorf("torn change: %d bytes with prefix %q", len(data), data[:min(len(data), 8)])
						return
					}
				}
			}()
			close(start)
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Error(err)
			}
		})
	}
}

// TestMetadataChangeDoesNotClobberConcurrentContent protects the metadata-only
// rule: chmod and utimes must change their own field, never restore an earlier
// row's size over a concurrently written content version. The read after each
// metadata call accepts either complete content version and no other bytes.
func TestMetadataChangeDoesNotClobberConcurrentContent(t *testing.T) {
	stores := newLayerStores(t)
	operations := map[string]func(*Store) error{
		"chmod": func(s *Store) error { return s.SetMode("/f", 0o640) },
		"utimes": func(s *Store) error {
			return s.SetTimes("/f", time.Unix(123, 456))
		},
	}
	for layer, store := range stores {
		for opname, mutate := range operations {
			t.Run(layer+"/"+opname, func(t *testing.T) {
				small := []byte("a")
				large := bytes.Repeat([]byte("b"), 2*contentChunkSize)
				if err := store.WriteFile("/f", small, 0o644); err != nil {
					t.Fatal(err)
				}
				const rounds = 2000
				start := make(chan struct{})
				errs := make(chan error, 2)
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					for i := 0; i < rounds; i++ {
						content := small
						if i%2 != 0 {
							content = large
						}
						if err := store.WriteFile("/f", content, 0o644); err != nil {
							errs <- err
							return
						}
					}
				}()
				go func() {
					defer wg.Done()
					<-start
					for i := 0; i < rounds; i++ {
						if err := mutate(store); err != nil {
							errs <- err
							return
						}
						data, err := store.ReadFile("/f")
						if err != nil {
							errs <- err
							return
						}
						if !bytes.Equal(data, small) && !bytes.Equal(data, large) {
							errs <- fmt.Errorf("metadata restored a stale row: %d bytes with prefix %q", len(data), data[:min(len(data), 8)])
							return
						}
					}
				}()
				close(start)
				wg.Wait()
				close(errs)
				for err := range errs {
					t.Error(err)
				}
			})
		}
	}
}
