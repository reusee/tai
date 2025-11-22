package codes

import (
	"github.com/reusee/dscope"
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/taiconfigs"
)

type Module struct {
	dscope.Module
	Generators generators.Module
	Configs    taiconfigs.Module
	GoCodes    gocodes.Module
	AnyTexts   anytexts.Module
}
