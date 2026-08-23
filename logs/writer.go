package logs

import (
	"io"
	"os"
	"testing"
)

type Writer io.Writer

// Writer provides the destination for log records. In tests — a non-nil
// *testing.T resolved from the scope, provided by modes.ForTest — records
// go to the running test's output writer, attributed to the test instead
// of raw stderr. In production the scope provides a nil *testing.T
// (modes.ModuleForProduction) and records go to stderr.
func (Module) Writer(t *testing.T) Writer {
	if t == nil {
		return os.Stderr
	}
	return t.Output()
}
