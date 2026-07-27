//go:build !linux

package main

// maybeRunInContainer is a no-op on non-Linux platforms.
// See TheoryOfContainerIsolation in main.go.
func maybeRunInContainer() {
}
