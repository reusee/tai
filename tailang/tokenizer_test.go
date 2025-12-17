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
			input: `"line1\nline2" 'tab\tquoted\'string'`,
			tokens: []TokenInfo{
				{TokenString, "line1\nline2"},
				{TokenString, "tab\tquoted'string"},
			},
		},
		{
			input: "& [ ] ( ) { }",
			tokens: []TokenInfo{
				{TokenIdentifier, "&"},
				{TokenSymbol, "["},
				{TokenSymbol, "]"},
				{TokenSymbol, "("},
				{TokenSymbol, ")"},
				{TokenSymbol, "{"},
				{TokenSymbol, "}"},
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
				{TokenIdentifier, "foo.bar"},
			},
		},
		{
			input: "foo&bar",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo&bar"},
			},
		},
		{
			input: "^",
			tokens: []TokenInfo{
				{TokenIdentifier, "^"},
			},
		},
		{
			input: "'unclosed",
			tokens: []TokenInfo{
				{TokenInvalid, "unclosed"},
			},
		},
		{
			input: "foo # comment \n bar",
			tokens: []TokenInfo{
				{TokenIdentifier, "foo"},
				{TokenIdentifier, "bar"},
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

func TestTokenizerNumbers(t *testing.T) {
	type TokenInfo struct {
		Kind TokenKind
		Text string
	}
	tests := []struct {
		input  string
		tokens []TokenInfo
	}{
		{
			input: "1_000 -123 1.5e-2 1E+5 +42",
			tokens: []TokenInfo{
				{TokenNumber, "1000"},
				{TokenNumber, "-123"},
				{TokenNumber, "1.5e-2"},
				{TokenNumber, "1E+5"},
				{TokenNumber, "+42"},
			},
		},
		{
			input: "1-1",
			tokens: []TokenInfo{
				{TokenNumber, "1"},
				{TokenNumber, "-1"},
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
		})
	}
}

func TestTokenizerEdgeCases(t *testing.T) {
	type TokenInfo struct {
		Kind TokenKind
		Text string
	}

	tests := []struct {
		name   string
		input  string
		tokens []TokenInfo
	}{
		{
			name:   "CommentAtEOF",
			input:  "foo # bar",
			tokens: []TokenInfo{{TokenIdentifier, "foo"}},
		},
		{
			name:   "Empty",
			input:  "   ",
			tokens: []TokenInfo{},
		},
		{
			name:   "OnlyComment",
			input:  "# just a comment",
			tokens: []TokenInfo{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokenizer := NewTokenizer(strings.NewReader(test.input))
			for i, expected := range test.tokens {
				token, err := tokenizer.Current()
				if err != nil {
					t.Fatalf("step %d: unexpected error: %v", i, err)
				}
				if token.Kind != expected.Kind {
					t.Errorf("step %d: expected kind %v, got %v", i, expected.Kind, token.Kind)
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
