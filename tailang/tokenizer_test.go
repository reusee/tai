package tailang

import (
	"strings"
	"testing"
)

func TestTokenizer(t *testing.T) {
	type TokenInfo struct {
		Kind TokenKind
		Text string
	}

	tests := []struct {
		input  string
		tokens []TokenInfo
	}{
		{
			input: "hello world",
			tokens: []TokenInfo{
				{TokenIdentifier, "hello"},
				{TokenIdentifier, "world"},
			},
		},
		{
			input: "  foo   bar  ",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo"},
				{TokenIdentifier, "bar"},
			},
		},
		{
			input: ".param1 .param-2",
			tokens: []TokenInfo{
				{TokenNamedParam, ".param1"},
				{TokenNamedParam, ".param-2"},
			},
		},
		{
			input: "123 45.67",
			tokens: []TokenInfo{
				{TokenNumber, "123"},
				{TokenNumber, "45.67"},
			},
		},
		{
			input: `'str1' "str2" ` + "`str3`",
			tokens: []TokenInfo{
				{TokenString, "str1"},
				{TokenString, "str2"},
				{TokenString, "str3"},
			},
		},
		{
			input: "& [ ]",
			tokens: []TokenInfo{
				{TokenSymbol, "&"},
				{TokenSymbol, "["},
				{TokenSymbol, "]"},
			},
		},
		{
			input: "foo-bar_baz",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo-bar_baz"},
			},
		},
		{
			input: "foo.bar",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo"},
				{TokenNamedParam, ".bar"},
			},
		},
		{
			input: "foo&bar",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo"},
				{TokenSymbol, "&"},
				{TokenIdentifier, "bar"},
			},
		},
		{
			input: "^",
			tokens: []TokenInfo{
				{TokenInvalid, "^"},
			},
		},
		{
			input: "'unclosed",
			tokens: []TokenInfo{
				{TokenInvalid, "unclosed"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			tokenizer := NewTokenizer(strings.NewReader(test.input))
			for i, expected := range test.tokens {
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
