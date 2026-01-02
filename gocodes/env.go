package gocodes

import (
	"os"

	"github.com/reusee/tai/configs"
)

type Envs []string

var _ configs.Configurable = Envs(nil)

func (e Envs) TaigoConfigurable() {
}

func (Module) Envs(
	loader configs.Loader,
) (ret Envs) {
	ret = os.Environ()
	for envs := range configs.All[Envs](loader, "go_envs") {
		ret = append(ret, envs...)
	}
	for envs := range configs.All[Envs](loader, "go.envs") {
		ret = append(ret, envs...)
	}
	return
}
