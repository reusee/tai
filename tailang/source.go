package tailang

type Source struct {
	Name    string
	Content string
}

func NewSource(name string, content string) *Source {
	return &Source{
		Name:    name,
		Content: content,
	}
}
