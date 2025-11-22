package nets

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
)

type Module struct {
	dscope.Module
	Configs configs.Module
	Logs    logs.Module
}
