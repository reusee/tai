package tailang

type Token struct {
	Kind TokenKind
	Text string
	Pos  Pos
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
	TokenUnquotedString
)

type Pos struct {
	Source *Source
	Line   int
	Column int
}
