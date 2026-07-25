package flags

import (
	"maps"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// TheoryOfConfigFlagParity documents the design principle that configuration
// options should be accessible through both command-line flags and CUE
// configuration files. configs.Load runs before flags.Parse in main.go, so
// flag values always override config values. For composite types (maps,
// lists), flags accumulate through repeated invocation while config files
// specify structured lists. API keys are exempt: they are config-only to
// avoid exposing secrets in process command-line listings.
const TheoryOfConfigFlagParity = `
Configuration options should be accessible through both command-line flags and
configuration files (CUE). Flags provide per-invocation overrides, while config
files provide persistent defaults. The configs.Load function runs before
flags.Parse, so flag values always override config values. For composite types
(maps, lists), flags accumulate values through repeated invocation, while config
files specify values as structured lists. API keys are exempt from this parity
principle: they are config-only and environment-variable-only to avoid exposing
secrets in process command-line listings.
`

// Effort configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = Effort("")

func (e Effort) ConfigPaths() []string {
	return []string{"effort"}
}

func (e Effort) HandleConfig(path string, values []*cue.Value) (any, error) {
	var s string
	if err := values[0].Decode(&s); err != nil {
		return nil, err
	}
	return Effort(s), nil
}

// Shell configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = Shell(false)

func (s Shell) ConfigPaths() []string {
	return []string{"shell"}
}

func (s Shell) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Shell(b), nil
}

// Thoughts configs.Config implementation. See TheoryOfConfigFlagParity.

var _ configs.Config = Thoughts{}

func (t Thoughts) ConfigPaths() []string {
	return []string{"thoughts"}
}

func (t Thoughts) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return Thoughts{Value: &b}, nil
}

// Ignore configs.Config implementation. Config values are a list of
// patterns; multiple config files are merged additively.
// See TheoryOfConfigFlagParity.

var _ configs.Config = Ignore(nil)

func (i Ignore) ConfigPaths() []string {
	return []string{"ignore"}
}

func (i Ignore) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Ignore, len(i))
	maps.Copy(ret, i)
	for _, v := range values {
		var patterns []string
		if err := v.Decode(&patterns); err != nil {
			return nil, err
		}
		for _, p := range patterns {
			ret[p] = true
		}
	}
	return ret, nil
}

// Files configs.Config implementation. Config values are a list of
// patterns; multiple config files are merged additively.
// See TheoryOfConfigFlagParity.

var _ configs.Config = Files(nil)

func (f Files) ConfigPaths() []string {
	return []string{"files"}
}

func (f Files) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Files, len(f))
	maps.Copy(ret, f)
	for _, v := range values {
		var patterns []string
		if err := v.Decode(&patterns); err != nil {
			return nil, err
		}
		for _, p := range patterns {
			ret[p] = true
		}
	}
	return ret, nil
}

// Focus configs.Config implementation. Config values are a list of
// aspects; multiple config files are merged additively with the
// existing flag-accumulated value.
// See TheoryOfConfigFlagParity.

var _ configs.Config = Focus(nil)

func (f Focus) ConfigPaths() []string {
	return []string{"focus"}
}

func (f Focus) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Focus, len(f))
	copy(ret, f)
	for _, v := range values {
		var items []string
		if err := v.Decode(&items); err != nil {
			return nil, err
		}
		ret = append(ret, items...)
	}
	return ret, nil
}

// Match configs.Config implementation. The "match_patterns" path
// provides a list of regex patterns; the legacy "match" path provides
// a single regex string. Both are merged additively with the existing
// flag-accumulated value. See TheoryOfConfigFlagParity.

var _ configs.Config = Match(nil)

func (m Match) ConfigPaths() []string {
	return []string{"match_patterns", "match"}
}

func (m Match) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := make(Match, len(m))
	maps.Copy(ret, m)
	for _, v := range values {
		if path == "match" {
			var s string
			if err := v.Decode(&s); err != nil {
				return nil, err
			}
			ret[s] = true
		} else {
			var patterns []string
			if err := v.Decode(&patterns); err != nil {
				return nil, err
			}
			for _, p := range patterns {
				ret[p] = true
			}
		}
	}
	return ret, nil
}
