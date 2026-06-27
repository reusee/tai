//go:build !linux

package taido

import (
	"fmt"

	"github.com/reusee/tai/logs"
)

// applySandbox is a no‑op on non‑Linux platforms.
// It returns an error to inform the user that sandboxing is not available.
func applySandbox(logger logs.Logger) error {
	return fmt.Errorf("sandbox not supported on this platform")
}
