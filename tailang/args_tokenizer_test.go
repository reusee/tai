package tailang

import "testing"

func TestArgsTokenizer(t *testing.T) {
	type TokenInfo struct {
		Kind TokenKind
		Text string
	}

	tests := []struct {
		name   string
		args   []string
		tokens []TokenInfo
	}{
		{
			name: "Symbols",
			args: []string{"[", "]", "(", ")", "{", "}"},
			tokens: []TokenInfo{
				{TokenSymbol, "["},
				{TokenSymbol, "]"},
				{TokenSymbol, "("},
				{TokenSymbol, ")"},
				{TokenSymbol, "{"},
				{TokenSymbol, "}"},
			},
		},
		{
			name: "Numbers",
			args: []string{"123", "45.67", "0", "0.1"},
			tokens: []TokenInfo{
				{TokenNumber, "123"},
				{TokenNumber, "45.67"},
				{TokenNumber, "0"},
				{TokenNumber, "0.1"},
			},
		},
		{
			name: "NotNumbers",
			args: []string{"1.2.3", "1a", "."},
			tokens: []TokenInfo{
				{TokenUnquotedString, "1.2.3"},
				{TokenUnquotedString, "1a"},
				{TokenUnquotedString, "."},
			},
		},
		{
			name: "NamedParams",
			args: []string{".foo", ".bar_baz"},
			tokens: []TokenInfo{
				{TokenNamedParam, ".foo"},
				{TokenNamedParam, ".bar_baz"},
			},
		},
		{
			name: "Keywords",
			args: []string{"def", "if", "true", "nil"},
			tokens: []TokenInfo{
				{TokenIdentifier, "def"},
				{TokenIdentifier, "if"},
				{TokenIdentifier, "true"},
				{TokenIdentifier, "nil"},
			},
		},
		{
			name: "UnquotedStrings",
			args: []string{"foo", "bar", "baz_qux"},
			tokens: []TokenInfo{
				{TokenUnquotedString, "foo"},
				{TokenUnquotedString, "bar"},
				{TokenUnquotedString, "baz_qux"},
			},
		},
		{
			name: "Mixed",
			args: []string{"def", "foo", "123", ".param", "[", "bar", "]"},
			tokens: []TokenInfo{
				{TokenIdentifier, "def"},
				{TokenUnquotedString, "foo"},
				{TokenNumber, "123"},
				{TokenNamedParam, ".param"},
				{TokenSymbol, "["},
				{TokenUnquotedString, "bar"},
				{TokenSymbol, "]"},
			},
		},
		{
			name:   "Empty",
			args:   []string{},
			tokens: []TokenInfo{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokenizer := NewArgsTokenizer(test.args)
			for i, expected := range test.tokens {
				// Check Current() multiple times to ensure idempotency without Consume()
				for j := 0; j < 3; j++ {
					token, err := tokenizer.Current()
					if err != nil {
						t.Fatalf("step %d: unexpected error: %v", i, err)
					}
					if token.Kind != expected.Kind {
						t.Errorf("step %d: expected kind %v, got %v (text: %q)", i, expected.Kind, token.Kind, token.Text)
					}
					if token.Text != expected.Text {
						t.Errorf("step %d: expected text %q, got %q", i, expected.Text, token.Text)
					}
				}
				tokenizer.Consume()
			}
			token, err := tokenizer.Current()
			if err != nil {
				t.Fatalf("eof: unexpected error: %v", err)
			}
			if token.Kind != TokenEOF {
				t.Errorf("expected EOF, got %v", token.Kind)
			}
		})
	}
}
