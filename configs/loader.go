package configs

import (
	"iter"
	"os"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

type Loader struct {
	getRoots func() ([]rootInfo, error)
}

func NewLoader(filePaths []string, schemaSrc string) Loader {
	return Loader{

		getRoots: sync.OnceValues(func() (ret []rootInfo, err error) {

			var schema cue.Value
			if schemaSrc != "" {
				ctx := cuecontext.New()
				schema = ctx.CompileString("close({" + schemaSrc + "})")
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
				value := ctx.CompileBytes(
					content,
					cue.Filename(filePath),
				)
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
