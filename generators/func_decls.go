package generators

import (
	"slices"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

type FuncDecls []FuncDecl

func (Module) FuncDecls() FuncDecls {
	return nil
}

var _ configs.Config = FuncDecls{}

func (f FuncDecls) ConfigPaths() []string {
	return []string{"functions"}
}

func (f FuncDecls) HandleConfig(path string, values []*cue.Value) (any, error) {
	ret := slices.Clone(f)
	for _, value := range values {
		var decls FuncDecls
		if err := value.Decode(&decls); err != nil {
			return nil, err
		}
		ret = append(ret, decls...)
	}
	return ret, nil
}
