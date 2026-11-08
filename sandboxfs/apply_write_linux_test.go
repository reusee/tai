//go:build linux

package sandboxfs

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// TestApplyRestoresTheHostModeAfterAFailedWrite protects
// TheoryOfApplyWriteBits: the owner's write bit is temporary, so a write the
// host refuses must not leave a permission change the session never requested.
func TestApplyRestoresTheHostModeAfterAFailedWrite(t *testing.T) {
	program, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skipf("read the helper executable: %v", err)
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "a.out")
	if err := os.WriteFile(host, program, 0o555); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(host, "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("start the helper executable: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	store, err := OpenDir(filepath.Join(dir, "record.d"), base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.WriteFile("/a.out", []byte("replacement"), 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(nil); !errors.Is(err, syscall.ETXTBSY) {
		t.Fatalf("apply error = %v, want ETXTBSY", err)
	}
	info, err := os.Stat(host)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o555 {
		t.Errorf("host mode after the failed write = %o, want 555", got)
	}
}
