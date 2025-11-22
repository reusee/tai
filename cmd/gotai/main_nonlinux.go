//go:build !linux

package main

func maybeRunInContainer() {
	// no-op on non-linux systems
}
