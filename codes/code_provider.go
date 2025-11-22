package codes

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/vars"
)

type CodeProviderName string

var codeProviderName CodeProviderName

func init() {
	cmds.Define("go", cmds.Func(func() {
		codeProviderName = "go"
	}).Desc("use gocodes context provider"))
	cmds.Define("any", cmds.Func(func() {
		codeProviderName = "any"
	}).Desc("use anytext context provider"))
}

func (Module) CodeProviderName() CodeProviderName {
	return vars.FirstNonZero(
		codeProviderName,
		"any",
	)
}

func (Module) CodeProvider(
	name CodeProviderName,
	goCodesProvider gocodes.CodeProvider,
	anyTextsProvider anytexts.CodeProvider,
) codetypes.CodeProvider {
	switch name {
	case "go":
		return goCodesProvider
	case "any":
		return anyTextsProvider
	}
	return nil
}
