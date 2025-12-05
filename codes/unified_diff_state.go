package codes

import (
	"fmt"
	"io"

	"github.com/reusee/tai/generators"
)

type UnifiedDiffState struct {
	upstream generators.State
	w        io.Writer
}

var _ generators.State = UnifiedDiffState{}

func (u UnifiedDiffState) AppendContent(content *generators.Content) (generators.State, error) {
	// copy
	ret := u

	for _, part := range content.Parts {
		call, ok := part.(generators.FuncCall)
		if !ok || call.Name != "apply_change" {
			continue
		}

		op := call.Args["operation"].(string)
		target := call.Args["target"].(string)
		path := call.Args["path"].(string)
		contentStr := ""
		if c, ok := call.Args["content"].(string); ok {
			contentStr = c
		}

		fmt.Fprintf(u.w, "[[[ %s %s IN %s\n%s\n]]]\n\n", op, target, path, contentStr)
	}

	var err error
	ret.upstream, err = u.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (u UnifiedDiffState) Contents() []*generators.Content {
	return u.upstream.Contents()
}

func (u UnifiedDiffState) Flush() (generators.State, error) {
	ret := u
	var err error
	ret.upstream, err = u.upstream.Flush()
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (u UnifiedDiffState) FuncMap() map[string]*generators.Func {
	return u.upstream.FuncMap()
}

func (u UnifiedDiffState) SystemPrompt() string {
	return u.upstream.SystemPrompt()
}

func (u UnifiedDiffState) Unwrap() generators.State {
	return u.upstream
}
