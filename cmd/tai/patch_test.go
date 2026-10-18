package main

import (
	"errors"
	"io"
	"iter"
	"os"
	"testing"

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/changes"
)

// TestPatchFailureReturnsError guards the contract that the patch command
// reports an apply failure as the command's error, never as a direct
// process exit: under the TUI the command runs on the session's goroutine,
// and a process exit would skip the session's terminal restoration and
// kill the display. A regression fails this test by terminating the test
// binary. See TheoryOfPingCommand.
func TestPatchFailureReturnsError(t *testing.T) {
	failing := func(root *os.Root, diffFilePath string) iter.Seq2[changes.ChangeBlock, error] {
		return func(yield func(changes.ChangeBlock, error) bool) {
			yield(changes.ChangeBlock{}, errors.New("bad change block"))
		}
	}
	// PatchCommand is an apps.App whose Main field holds the function
	// value untyped (any); the assertion to the concrete function type
	// restores the compile-time signature check at the call and fails
	// loudly here when the main signature changes.
	mainFn := PatchCommand.Main.(func(Output, changes.ApplyDiffFile) apps.MainErr)
	err := mainFn(Output(io.Discard), failing)
	if err == nil {
		t.Fatal("expected a failed apply to return an error instead of exiting")
	}
}
