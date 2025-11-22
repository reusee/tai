package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

func TestGetDefaultGenerator(t *testing.T) {
	loader := configs.NewLoader([]string{}, "")
	dscope.New(
		new(Module),
		&loader,
		modes.ForTest(t),
	).Call(func(
		get GetDefaultGenerator,
	) {
		_, err := get()
		if err != nil {
			t.Fatal(err)
		}
	})
}
