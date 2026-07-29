package codes

import (
	"github.com/reusee/tai/anytexts"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/gocodes"
)

type CodeProviderName string

func (Module) CodeProviderName() CodeProviderName {
	return "any"
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
