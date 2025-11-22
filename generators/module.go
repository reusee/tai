package generators

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
)

type Module struct {
	dscope.Module
	Configs configs.Module
	Nets    nets.Module
	Logs    logs.Module
	Debugs  debugs.Module
}
