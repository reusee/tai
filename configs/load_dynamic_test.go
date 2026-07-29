package configs

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"github.com/reusee/dscope"
)

// dynConfig is a Config that resolves its CUE path dynamically via
// DynamicPathsConfig. Instead of returning static paths from ConfigPaths,
// it returns a function from ConfigPathsFunc whose arguments are resolved
// from the dscope scope, allowing the path to depend on other scope values.
type dynConfig struct {
	Value string
}

func (dynConfig) ConfigPaths() []string {
	// Not called for DynamicPathsConfig; ConfigPathsFunc is used instead.
	return nil
}

func (dynConfig) ConfigPathsFunc() any {
	// The returned function takes a scope dependency (dynPathPrefix)
	// and produces the CUE path at load time.
	return func(prefix dynPathPrefix) []string {
		return []string{prefix.Prefix}
	}
}

func (dynConfig) HandleConfig(path string, values []*cue.Value) (any, error) {
	for _, v := range values {
		var s string
		if err := v.Decode(&s); err != nil {
			continue
		}
		ret := dynConfig{Value: s}
		return &ret, nil
	}
	return nil, nil
}

// dynPathPrefix is a scope dependency that provides the dynamic CUE path.
type dynPathPrefix struct {
	Prefix string
}

func TestLoadDynamicPathsConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.cue")
	if err := os.WriteFile(configPath, []byte(`str: "bar"`), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader([]string{configPath}, LoaderConfig{Schema: testSchema})

	scope := dscope.New(
		func() dynConfig { return dynConfig{} },
		func() dynPathPrefix { return dynPathPrefix{Prefix: "str"} },
	)

	scope, err := Load(loader, scope)
	if err != nil {
		t.Fatal(err)
	}

	scope.Call(func(config dynConfig) {
		if config.Value != "bar" {
			t.Fatalf("expected %q, got %q", "bar", config.Value)
		}
	})
}

func TestLoadDynamicPathsConfigReevaluates(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test.cue")
	if err := os.WriteFile(configPath, []byte(`str: "bar"
secondary: "baz"`), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader([]string{configPath}, LoaderConfig{
		Schema: "str?: string\nsecondary?: string",
	})

	scope := dscope.New(
		func() dynConfig { return dynConfig{} },
		func() dynPathPrefix { return dynPathPrefix{Prefix: "str"} },
	)

	scope, err := Load(loader, scope)
	if err != nil {
		t.Fatal(err)
	}

	scope.Call(func(config dynConfig) {
		if config.Value != "bar" {
			t.Fatalf("expected %q, got %q", "bar", config.Value)
		}
	})

	// Fork with a different dependency value; the provider should
	// re-evaluate and pick up the new path's config value.
	scope = scope.Fork(func() dynPathPrefix {
		return dynPathPrefix{Prefix: "secondary"}
	})

	scope.Call(func(config dynConfig) {
		if config.Value != "baz" {
			t.Fatalf("expected %q after fork, got %q", "baz", config.Value)
		}
	})
}
