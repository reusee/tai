package debugs

import (
	"testing"

	"github.com/reusee/dscope"
)

func TestTap(t *testing.T) {
	dscope.New(
		new(Module),
	).Call(func(
		tap Tap,
	) {
		tap(t.Context(), "test", map[string]any{
			"foo": 42,
		})
	})
}
