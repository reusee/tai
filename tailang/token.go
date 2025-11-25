package tailang

type Token struct {
	Kind TokenKind
	Text string
}

type TokenKind uint8

const (
	TokenInvalid TokenKind = iota
	TokenIdentifier
	TokenString
	TokenNumber
)
