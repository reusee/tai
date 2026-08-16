package generators

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
)

func TestGetDefaultFastModel(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
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

func TestModelFamilyDerivesFromDefaultGenerator(t *testing.T) {
	// The default ModelFamily provider derives the family from the
	// resolved default generator, so no customization is needed. A
	// user-defined generator with a family produces a non-empty family.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.cue")
	configContent := `generators: [
  {
    name: "mygen"
    type: "gemini"
    model: "models/my-model"
    family: "my-family"
  },
]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() configs.Loader {
			return configs.NewLoader([]string{configPath}, configs.LoaderConfig{})
		},
		func() flags.ModelName { return "mygen" },
	).Call(func(
		modelFamily ModelFamily,
	) {
		if modelFamily != "my-family" {
			t.Fatalf("expected family %q, got %q", "my-family", modelFamily)
		}
	})
}
