package configs

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"github.com/reusee/dscope"
)

// overrideConfig verifies that later ConfigPaths override earlier ones.
type overrideConfig struct {
	Value string
}

func (overrideConfig) ConfigPaths() []string {
	return []string{"primary", "secondary"}
}

func (overrideConfig) HandleConfig(path string, values []*cue.Value) (any, error) {
	for _, v := range values {
		var s string
		if err := v.Decode(&s); err != nil {
			continue
		}
		ret := overrideConfig{Value: s}
		return &ret, nil
	}
	return nil, nil
}

func TestLoadLastPathWins(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.cue")
	if err := os.WriteFile(configPath, []byte(`primary: "first"
secondary: "second"`), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader([]string{configPath}, LoaderConfig{
		Schema: "primary?: string\nsecondary?: string",
	})

	scope := dscope.New(
		func() overrideConfig { return overrideConfig{} },
	)

	scope, err := Load(loader, scope)
	if err != nil {
		t.Fatal(err)
	}

	scope.Call(func(config overrideConfig) {
		if config.Value != "second" {
			t.Fatalf("expected %q (last path wins), got %q", "second", config.Value)
		}
	})
}
