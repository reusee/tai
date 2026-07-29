//go:build !linux

package security

// MaybeRunInContainer is a no-op on non-Linux platforms.
// See TheoryOfContainerIsolation in container_linux.go.
func MaybeRunInContainer() {}
