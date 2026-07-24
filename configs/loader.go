package configs

import (
	"encoding/json"
	"iter"
	"os"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

const TheoryOfConfigGlobals = `
Config globals inject Go values into the CUE evaluation context so config
files can reference them by path (e.g., prompts.fiction resolves to a Go
string injected under the "prompts" key). This bridges Go's compile-time
constants with CUE's configuration language, allowing config files to stay
in sync with Go-defined prompts, templates, and other values without
duplication.

CUE resolves references at compile time within a lexical scope. Compiling
the file as a standalone value creates an isolated scope where references
to fields not in the file itself are errors. Text concatenation and
post-compilation Unify both fail because CUE resolves references during
compilation, not during unification.

The solution uses cue.Scope: globals are marshaled to JSON (valid CUE) and
compiled as a CUE value. This value is passed as the scope option when
compiling each config file, making the globals' fields available as the
enclosing scope for reference resolution. References like prompts.fiction
resolve against the globals during compilation, and the resulting value
includes both the globals' fields and the file's fields.

Globals are also embedded inside the close() schema so the closed schema
accepts them as known fields. The closed schema still rejects fields that
are neither in the schema nor in the globals, preserving typo detection
for user-defined config fields.

When no globals are provided, behavior is identical to the prior
implementation: the file content is compiled directly with CompileBytes.
`

type Loader struct {
	getRoots func() ([]rootInfo, error)
}

// LoaderConfig holds configuration for creating a Loader.
type LoaderConfig struct {
	// Schema is the CUE schema source used to validate loaded configuration files.
	// If empty, no validation is performed.
	Schema string
	// Globals provides pre-defined values for the CUE evaluation context.
	// Config files can reference these values by their key path (e.g.,
	// prompts.erotica). Globals are marshaled to JSON (valid CUE) and
	// compiled as a CUE value that serves as the scope for each file
	// compilation, so references resolve at compile time.
	// See TheoryOfConfigGlobals.
	Globals map[string]any
}

func NewLoader(filePaths []string, cfg LoaderConfig) Loader {
	return Loader{

		getRoots: sync.OnceValues(func() (ret []rootInfo, err error) {

			// Marshal globals to JSON (valid CUE) for use as a CUE
			// scope value and for embedding in the closed schema.
			// See TheoryOfConfigGlobals.
			var globalsJSON []byte
			if len(cfg.Globals) > 0 {
				globalsJSON, err = json.Marshal(cfg.Globals)
				if err != nil {
					return nil, err
				}
			}

			var schema cue.Value
			if cfg.Schema != "" {
				ctx := cuecontext.New()
				// Embed globals inside close() so the closed schema
				// accepts global fields as known fields. Without this,
				// close() would reject config files that reference
				// global keys. See TheoryOfConfigGlobals.
				schemaSource := "close({\n" + cfg.Schema
				if len(globalsJSON) > 0 {
					schemaSource += "\n" + string(globalsJSON)
				}
				schemaSource += "\n})"
				schema = ctx.CompileString(schemaSource)
				if err := schema.Err(); err != nil {
					return nil, err
				}
			}

			for _, filePath := range filePaths {
				content, err := os.ReadFile(filePath)
				if err != nil {
					return nil, err
				}

				ctx := cuecontext.New()

				// When globals are present, compile them as a CUE value
				// and use cue.Scope to make their fields available as the
				// enclosing scope for the file's reference resolution.
				// CUE resolves references during compilation, not during
				// post-compilation Unify, so the scope must be set at
				// compile time. The resulting value includes both the
				// globals' fields and the file's fields.
				//
				// When no globals are provided, compile directly with
				// CompileBytes, preserving the prior code path.
				// See TheoryOfConfigGlobals.
				var value cue.Value
				if len(globalsJSON) > 0 {
					globalsValue := ctx.CompileString(string(globalsJSON))
					if err := globalsValue.Err(); err != nil {
						return nil, err
					}
					value = ctx.CompileString(
						string(content),
						cue.Scope(globalsValue),
						cue.Filename(filePath),
					)
				} else {
					value = ctx.CompileBytes(content, cue.Filename(filePath))
				}
				if err = value.Err(); err != nil {
					return nil, err
				}

				if schema.Exists() {
					if err := schema.Unify(value).Validate(); err != nil {
						return nil, err
					}
				}

				ret = append(ret, rootInfo{
					value: value,
					path:  filePath,
				})
			}

			return
		}),
	}
}

type rootInfo struct {
	value cue.Value
	path  string
}

func (l Loader) IterCueValues(path string) iter.Seq2[*cue.Value, error] {
	return func(yield func(*cue.Value, error) bool) {
		roots, err := l.getRoots()
		if err != nil {
			yield(nil, err)
			return
		}

		cuePath := cue.ParsePath(path)
		for _, info := range roots {
			value := info.value.LookupPath(cuePath)
			if err := value.Err(); err == nil {
				if !yield(&value, nil) {
					break
				}
			}
		}
	}
}

func (l Loader) AssignFirst(path string, target any) error {
	roots, err := l.getRoots()
	if err != nil {
		return err
	}

	cuePath := cue.ParsePath(path)
	for _, info := range roots {
		value := info.value.LookupPath(cuePath)
		if err := value.Err(); err == nil {
			if err := value.Decode(target); err != nil {
				return err
			}
			return nil
		}
	}

	return ErrValueNotFound
}
