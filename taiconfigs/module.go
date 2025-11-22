package taiconfigs

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/logs"
)

type Module struct {
	dscope.Module
	Logs logs.Module
}
