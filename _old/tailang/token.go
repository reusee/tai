package tailang

type Token struct {
	Kind  TokenKind
	Text  string
	Pos   Pos
	Value any
}

type TokenKind uint8

const (
	TokenInvalid TokenKind = iota
	TokenIdentifier
	TokenString
	TokenNumber
	TokenEOF
	TokenNamedParam
	TokenSymbol
)

var EOFToken = &Token{
	Kind: TokenEOF,
}
