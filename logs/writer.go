package logs

import (
	"io"
	"os"
)

type Writer io.Writer

func (Module) Writer() Writer {
	return os.Stderr
}
