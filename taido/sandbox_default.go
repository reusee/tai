//go:build !linux

package taido

import "github.com/reusee/tai/logs"

// applySandbox is a no-op on non-Linux platforms.
func applySandbox(logger logs.Logger) error {
	return nil
}
