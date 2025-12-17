package tailang

type TokenStream interface {
	Current() (*Token, error)
	Consume()
}
