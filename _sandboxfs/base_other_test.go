package sandboxfs

import (
	"os"
	"os/exec"
	"testing"
)

// TestPortableBaseFallbackBuilds protects TheoryOfBaseFdAccess's fallback
// contract: a platform without the Linux descriptor must still compile the
// same store, with every base fast-path entry deferring to the os.Root walk.
func TestPortableBaseFallbackBuilds(t *testing.T) {
	cmd := exec.Command("go", "build")
	cmd.Env = append(os.Environ(),
		"GOOS=darwin",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("portable store build: %v\n%s", err, output)
	}
}
