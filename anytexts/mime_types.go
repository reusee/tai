package anytexts

import (
	"maps"

	"github.com/reusee/tai/flags"
)

type IncludeMimeTypes map[string]bool

func (Module) IncludeMimeTypes() IncludeMimeTypes {
	return make(IncludeMimeTypes)
}

var _ flags.Flag = IncludeMimeTypes{}

func (i IncludeMimeTypes) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	m := maps.Clone(i)
	switch key {
	case "-pdf":
		m["application/pdf"] = true
	}
	return m, args, nil
}

func (i IncludeMimeTypes) Keys() []string {
	return []string{
		"-pdf",
	}
}
