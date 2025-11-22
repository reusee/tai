package generators

import (
	"sync"

	"github.com/reusee/tai/configs"
)

type GeneratorSpec struct {
	Name string `json:"name"`
	Type string `json:"type"`
	GeneratorArgs
}

type GetGeneratorSpecs func() ([]GeneratorSpec, error)

func (Module) GetGeneratorSpecs(
	loader configs.Loader,
) GetGeneratorSpecs {
	return sync.OnceValues(func() (ret []GeneratorSpec, err error) {
		for value, err := range loader.IterCueValues("generators") {
			if err != nil {
				return nil, err
			}
			var specs []GeneratorSpec
			if err := value.Decode(&specs); err != nil {
				return nil, err
			}
			ret = append(ret, specs...)
		}
		return
	})
}
