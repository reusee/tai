package generators

import (
	"context"
	"sync"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
)

type GetGeneratorSpecs func() ([]Spec, error)

func (Module) GetGeneratorSpecs(
	loader configs.Loader,
	logger logs.Logger,
) GetGeneratorSpecs {
	return sync.OnceValues(func() (ret []Spec, err error) {
		logger.InfoContext(context.Background(), "loading generator specs")
		for value, err := range loader.IterCueValues("generators") {
			if err != nil {
				logger.WarnContext(context.Background(), "loading generators config failed",
					"error", err,
				)
				return nil, err
			}
			var specs []Spec
			if err := value.Decode(&specs); err != nil {
				logger.WarnContext(context.Background(), "decoding generators config failed",
					"error", err,
				)
				return nil, err
			}
			logger.InfoContext(context.Background(), "generators config decoded",
				"count", len(specs),
			)
			ret = append(ret, specs...)
		}
		logger.InfoContext(context.Background(), "generator specs loaded",
			"count", len(ret),
		)
		return
	})
}
