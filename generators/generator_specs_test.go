package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

func TestGeneratorSpecs(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		dscope.Provide(configs.NewLoader(nil, "")),
		new(Module),
	).Fork(
		func() configs.Loader {
			return configs.NewLoader([]string{"test_generator_specs.cue"}, "")
		},
	).Call(func(
		get GetGenerator,
	) {

		_, err := get("foo")
		if err != nil {
			t.Fatal(err)
		}

	})
}
