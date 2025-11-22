package generators

import (
	"fmt"
	"slices"
)

type Prompts struct {
	systemPrompt string
	contents     []*Content
}

func NewPrompts(systemPrompt string, contents []*Content) Prompts {
	return Prompts{
		systemPrompt: systemPrompt,
		contents:     contents,
	}
}

var _ State = Prompts{}

func (p Prompts) AppendContent(content *Content) (State, error) {
	if content.Role == "" {
		panic(fmt.Errorf("empty role: %+v", content))
	}

	// copy
	ret := p
	ret.contents = slices.Clone(p.contents)

	// try merge
	if len(ret.contents) > 0 {
		newContent, ok := ret.contents[len(ret.contents)-1].Merge(content)
		if ok {
			ret.contents[len(ret.contents)-1] = newContent
			return ret, nil
		}
	}

	// append
	ret.contents = append(ret.contents, content)
	return ret, nil
}

func (p Prompts) Contents() []*Content {
	return p.contents
}

func (p Prompts) FuncMap() map[string]*Func {
	return map[string]*Func{}
}

func (p Prompts) SystemPrompt() string {
	return p.systemPrompt
}

func (p Prompts) Flush() (State, error) {
	return p, nil
}

func (p Prompts) Unwrap() State {
	return nil
}
