package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
)

func TestGetDefaultFastModel(t *testing.T) {
	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		new(flags.FastModelName("gemini-flash")),
	).Call(func(
		getDefaultFastModel GetDefaultFastModel,
	) {
		gen, err := getDefaultFastModel()
		if err != nil {
			t.Fatal(err)
		}
		if gen == nil {
			t.Fatal("expected non-nil generator")
		}
		// Default fallback is "gemini-flash" which maps to gemini-flash-latest
		if gen.Spec().Model != "models/gemini-flash-latest" {
			t.Fatalf("expected models/gemini-flash-latest, got %s", gen.Spec().Model)
		}
	})
}
