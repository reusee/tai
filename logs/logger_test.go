package logs

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestHandler(t *testing.T) {
	dscope.New(new(Module)).Call(func(
		logger Logger,
	) {
		logger.Info("test", "hello", "world!")
	})
}
