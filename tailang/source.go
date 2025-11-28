package tailang

import "strings"

type Source struct {
	Name    string
	Content string
	Lines   []string
}

func NewSource(name string, content string) *Source {
	return &Source{
		Name:    name,
		Content: content,
		Lines:   strings.Split(content, "\n"),
	}
}
