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

// TokenizeHeader parses a function-call block header from s.
// It returns the parsed token, the number of bytes consumed, and whether a valid header was found.
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

		paramNameStart := pos
		for pos < n && isHeaderNameChar(s[pos]) {
			pos++
		}
		paramName := s[paramNameStart:pos]
		if paramName == "" {
			return HeaderToken{}, 0, false
		}

		for pos < n && isHeaderSpace(s[pos]) {
			pos++
		}
		if pos >= n || s[pos] != '=' {
			return HeaderToken{}, 0, false
		}
		pos++ // skip '='

		for pos < n && isHeaderSpace(s[pos]) {
			pos++
		}
		if pos >= n {
			return HeaderToken{}, 0, false
		}

		if s[pos] == '"' || s[pos] == '\'' {
			quote := s[pos]
			pos++ // skip opening quote
			var valBuilder strings.Builder
			closed := false
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
					pos++ // skip closing quote
					closed = true
					break
				}
				valBuilder.WriteByte(c)
				pos++
			}
			if !closed {
				return HeaderToken{}, 0, false
			}
			attrs[paramName] = valBuilder.String()
		} else {
			valStart := pos
			for pos < n && !isHeaderSpace(s[pos]) && s[pos] != ',' && s[pos] != ')' {
				pos++
			}
			val := s[valStart:pos]
			if val == "" {
				return HeaderToken{}, 0, false
			}
			attrs[paramName] = val
		}
	}
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

// extractKindName extracts the kind name from a possibly-incomplete header string.
func extractKindName(s string) string {
	s = strings.TrimSpace(s)
	start := 0
	for start < len(s) && isHeaderNameChar(s[start]) {
		start++
	}
	return s[:start]
}
