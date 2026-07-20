package codes

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/gocodes"
	"github.com/reusee/tai/vars"
)

type CodeProviderName string

var codeProviderName CodeProviderName

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
