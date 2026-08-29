package blocks

import (
	"strings"
)

const TheoryOfHeaderTokenizing = `
Header tokenizing parses function-call block headers from lines.
The format is kind(param1="value1", param2="value2") or bare kind
when no parameters are needed. Parameter values may be quoted with single
or double quotes, supporting standard escape sequences (\n, \t, \r, \",
\', \\). Parameter names and kind names support letters, digits, hyphens,
underscores, periods, and colons. Whitespace and optional commas separate
parameters. A tag is complete only when matching closing quotes and
parentheses are found outside quoted values.
`

type HeaderToken struct {
	Kind       string
	Attributes map[string]string
}

func isHeaderSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isHeaderNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.' || b == ':'
}

// TheoryOfAttributeOnlyHeaders documents the attribute-only header form: an
// opening marker whose header is a paren-less parameter list carrying the
// block kind in the kind attribute. See the constant body for the rule and
// its rationale.

const TheoryOfAttributeOnlyHeaders = `
Models occasionally emit block opening markers whose header is a bare
parameter list: <<DELIMITER kind="summary" instead of <<DELIMITER
kind(...) or a bare kind. The tokenizer accepts this shape: after the
first name token it expects '(', end of line, or '='; on '=' the scanned
name becomes the first parameter name of a paren-less parameter list,
parsed with the same parameter grammar as the function-call form, and the
kind attribute's value becomes the block Kind. The list must consume the
whole line, and a missing or empty kind attribute is malformed, so the
ordinary parse-error feedback applies instead of a silent drop.

This is a recovery path for nonconforming output, not an advertised
format: no prompt teaches it, and the function-call and bare-kind forms
remain the only taught forms. Like the lenient opening markers, it exists
because the delimiter marks the line as an intended block opening, so a
recognizable near-miss is parsed rather than discarded.
`

// TokenizeHeader parses a block header from s — the function-call form
// kind(param="value", ...), the bare kind form, or the attribute-only form
// whose paren-less parameter list carries the kind in the kind attribute —
// and returns the parsed token, the number of bytes consumed, and whether a
// valid header was found.
func TokenizeHeader(s string) (token HeaderToken, consumed int, ok bool) {
	pos := 0
	n := len(s)

	for pos < n && isHeaderSpace(s[pos]) {
		pos++
	}
	if pos >= n {
		return HeaderToken{}, 0, false
	}

	kindStart := pos
	for pos < n && isHeaderNameChar(s[pos]) {
		pos++
	}
	kind := s[kindStart:pos]
	if kind == "" {
		return HeaderToken{}, 0, false
	}

	for pos < n && isHeaderSpace(s[pos]) {
		pos++
	}

	if pos < n && s[pos] == '=' {
		// Attribute-only form: the scanned name is the first
		// parameter name of a paren-less parameter list. See
		// TheoryOfAttributeOnlyHeaders.
		return tokenizeAttributeOnlyHeader(s, pos, kind)
	}

	if pos >= n || s[pos] != '(' {
		return HeaderToken{
			Kind:       kind,
			Attributes: nil,
		}, pos, true
	}

	pos++ // skip '('
	attrs := make(map[string]string)

	for {
		for pos < n && (isHeaderSpace(s[pos]) || s[pos] == ',') {
			pos++
		}
		if pos >= n {
			return HeaderToken{}, 0, false
		}

		if s[pos] == ')' {
			pos++ // skip ')'
			return HeaderToken{
				Kind:       kind,
				Attributes: attrs,
			}, pos, true
		}

		name, value, newPos, valid := parseHeaderParameter(s, pos, "")
		if !valid {
			return HeaderToken{}, 0, false
		}
		pos = newPos
		attrs[name] = value
	}
}

// tokenizeAttributeOnlyHeader parses the attribute-only header form: a
// paren-less parameter list whose kind attribute names the block kind.
// firstParamName is the parameter name already scanned by the caller, and
// pos points at its '='. See TheoryOfAttributeOnlyHeaders.
func tokenizeAttributeOnlyHeader(s string, pos int, firstParamName string) (token HeaderToken, consumed int, ok bool) {
	n := len(s)
	attrs := make(map[string]string)
	paramName := firstParamName
	for {
		name, value, newPos, valid := parseHeaderParameter(s, pos, paramName)
		if !valid {
			return HeaderToken{}, 0, false
		}
		attrs[name] = value
		pos = newPos
		paramName = ""

		for pos < n && (isHeaderSpace(s[pos]) || s[pos] == ',') {
			pos++
		}
		if pos >= n {
			break
		}
	}
	kind := attrs["kind"]
	if kind == "" {
		return HeaderToken{}, 0, false
	}
	return HeaderToken{
		Kind:       kind,
		Attributes: attrs,
	}, pos, true
}

// parseHeader parses a function-call block header and ensures no trailing content.
func parseHeader(s string) (kind string, attrs map[string]string, ok bool) {
	token, consumed, ok2 := TokenizeHeader(s)
	if !ok2 {
		return "", nil, false
	}
	if strings.TrimSpace(s[consumed:]) != "" {
		return "", nil, false
	}
	return token.Kind, token.Attributes, true
}

// parseHeaderParameter parses one name=value parameter. When firstParamName
// is non-empty it is the already-scanned parameter name and pos points at
// its '='; otherwise pos points at the parameter name, which is scanned
// here. The grammar matches the function-call parameter loop: optional
// spaces around '=', and a double- or single-quoted value with escape
// sequences, or an unquoted value terminated by whitespace, ',', or ')'.
func parseHeaderParameter(s string, pos int, firstParamName string) (name string, value string, newPos int, ok bool) {
	n := len(s)
	paramName := firstParamName
	if paramName == "" {
		paramNameStart := pos
		for pos < n && isHeaderNameChar(s[pos]) {
			pos++
		}
		paramName = s[paramNameStart:pos]
		if paramName == "" {
			return "", "", 0, false
		}
	}

	for pos < n && isHeaderSpace(s[pos]) {
		pos++
	}
	if pos >= n || s[pos] != '=' {
		return "", "", 0, false
	}
	pos++ // skip '='

	for pos < n && isHeaderSpace(s[pos]) {
		pos++
	}
	if pos >= n {
		return "", "", 0, false
	}

	if s[pos] == '"' || s[pos] == '\'' {
		value, newPos, ok := parseQuotedHeaderValue(s, pos)
		if !ok {
			return "", "", 0, false
		}
		return paramName, value, newPos, true
	}

	valStart := pos
	for pos < n && !isHeaderSpace(s[pos]) && s[pos] != ',' && s[pos] != ')' {
		pos++
	}
	val := s[valStart:pos]
	if val == "" {
		return "", "", 0, false
	}
	return paramName, val, pos, true
}

// extractKindName extracts the kind name from a possibly-incomplete header
// string. A header that tokenizes yields its tokenized kind — for the
// attribute-only form, the value of the kind attribute; an incomplete or
// malformed header falls back to its first name token, which is the kind in
// the function-call and bare forms and the first parameter name in the
// attribute-only form.
func extractKindName(s string) string {
	s = strings.TrimSpace(s)
	if token, _, ok := TokenizeHeader(s); ok {
		return token.Kind
	}
	start := 0
	for start < len(s) && isHeaderNameChar(s[start]) {
		start++
	}
	return s[:start]
}

// parseQuotedHeaderValue parses a quoted parameter value starting at the
// opening quote at pos. Escape sequences (\n, \t, \r, \\", \\') are decoded;
// any other escaped character keeps its backslash. It returns the decoded
// value and the position after the closing quote. An unclosed quote fails.
func parseQuotedHeaderValue(s string, pos int) (value string, newPos int, ok bool) {
	quote := s[pos]
	pos++ // skip opening quote
	var valBuilder strings.Builder
	n := len(s)
	for pos < n {
		c := s[pos]
		if c == '\\' && pos+1 < n {
			pos++
			next := s[pos]
			switch next {
			case 'n':
				valBuilder.WriteByte('\n')
			case 't':
				valBuilder.WriteByte('\t')
			case 'r':
				valBuilder.WriteByte('\r')
			case '\\':
				valBuilder.WriteByte('\\')
			case '"':
				valBuilder.WriteByte('"')
			case '\'':
				valBuilder.WriteByte('\'')
			default:
				valBuilder.WriteByte('\\')
				valBuilder.WriteByte(next)
			}
			pos++
			continue
		}
		if c == quote {
			return valBuilder.String(), pos + 1, true
		}
		valBuilder.WriteByte(c)
		pos++
	}
	return "", 0, false
}
