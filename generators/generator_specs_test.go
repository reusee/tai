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
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
		new(Module),
	).Fork(
		func() configs.Loader {
			return configs.NewLoader([]string{"test_generator_specs.cue"}, configs.LoaderConfig{})
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
