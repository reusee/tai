package generators

import (
	"sync"

	"github.com/reusee/tai/configs"
)

type GetGeneratorSpecs func() ([]Spec, error)

func (Module) GetGeneratorSpecs(
	loader configs.Loader,
) GetGeneratorSpecs {
	return sync.OnceValues(func() (ret []Spec, err error) {
		for value, err := range loader.IterCueValues("generators") {
			if err != nil {
				return nil, err
			}
			var specs []Spec
			if err := value.Decode(&specs); err != nil {
				return nil, err
			}
			ret = append(ret, specs...)
		}
		return
	})
}
