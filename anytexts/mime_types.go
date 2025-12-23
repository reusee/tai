package anytexts

import "github.com/reusee/tai/cmds"

var includeNonTextMimeTypes = map[string]bool{}

func init() {
	cmds.Define("-pdf", cmds.Func(func() {
		includeNonTextMimeTypes["application/pdf"] = true
	}))
}
