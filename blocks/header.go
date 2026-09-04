package blocks

import (
	"net/url"
	"strings"
)

const TheoryOfHeaderTokenizing = `
Header tokenizing parses block headers in RFC 3986 URI syntax. The
scheme is the block kind. The accepted forms are: bare kind (kind),
scheme only (kind:), scheme with path (kind:path), scheme with query
(kind:?key=value&key2=value2), and scheme with path and query
(kind:path?query). kind?query without the colon is rejected: under
RFC 3986 it is a relative reference with no scheme, so the kind would
be lost. Query keys and values are percent-decoded; a pair without
'=', an empty key, or a malformed escape rejects the header. A '#'
fragment ends the scan, and the remainder is trailing content that the
caller rejects, so fragments are unused. Kind names support letters,
digits, hyphens, underscores, and periods; the colon ends the name.
`

// HeaderToken is one parsed block header: the kind, the optional URI
// path, and the query parameters.
type HeaderToken struct {
	Kind       string
	Path       string
	Attributes map[string]string
}

func isHeaderSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isHeaderNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.'
}

// TokenizeHeader parses a block header from s and returns the parsed
// token, the number of bytes consumed, and whether a valid header was
// found. The format is RFC 3986 URI syntax: the scheme is the block
// kind, followed by an optional colon-led body of an optional path and
// an optional query of key=value pairs joined by '&'. The bare kind
// without the colon is also accepted. A query without the colon is
// rejected because it carries no scheme. Content after the header is
// left unconsumed for the caller to reject. See
// TheoryOfHeaderTokenizing.
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

	if pos < n {
		switch s[pos] {
		case ':':
			return tokenizeURIHeader(s, pos+1, kind)
		case '?':
			// Under RFC 3986, kind?query without the ':' is a
			// relative reference carrying no scheme, so the kind
			// would be lost. Rejected; the taught form is
			// kind:?query.
			return HeaderToken{}, 0, false
		}
	}

	for pos < n && isHeaderSpace(s[pos]) {
		pos++
	}

	return HeaderToken{
		Kind:       kind,
		Attributes: nil,
	}, pos, true
}

// tokenizeURIHeader parses the header body after the kind and its ':':
// an optional path, then an optional query of key=value pairs joined by
// '&'. Keys and values are percent-decoded; a pair without '=', an
// empty key, or a malformed escape rejects the header. A '#' fragment
// or whitespace ends the scan; what follows is trailing content
// reported by parseHeader. See TheoryOfHeaderTokenizing.
func tokenizeURIHeader(s string, pos int, kind string) (token HeaderToken, consumed int, ok bool) {
	n := len(s)
	pathStart := pos
	for pos < n && s[pos] != '?' && s[pos] != '#' && !isHeaderSpace(s[pos]) {
		pos++
	}
	path := s[pathStart:pos]

	attrs := map[string]string{}
	if pos < n && s[pos] == '?' {
		pos++
		for pos < n {
			segmentEnd := pos
			for segmentEnd < n && s[segmentEnd] != '&' &&
				s[segmentEnd] != '#' && !isHeaderSpace(s[segmentEnd]) {
				segmentEnd++
			}
			segment := s[pos:segmentEnd]
			eq := strings.IndexByte(segment, '=')
			if eq <= 0 {
				return HeaderToken{}, 0, false
			}
			key, err := url.PathUnescape(segment[:eq])
			if err != nil {
				return HeaderToken{}, 0, false
			}
			value, err := url.PathUnescape(segment[eq+1:])
			if err != nil {
				return HeaderToken{}, 0, false
			}
			attrs[key] = value
			pos = segmentEnd
			if pos < n && s[pos] == '&' {
				pos++
				continue
			}
			break
		}
	}

	return HeaderToken{
		Kind:       kind,
		Path:       path,
		Attributes: attrs,
	}, pos, true
}

// parseHeader parses a block header in RFC 3986 URI form and ensures no
// trailing content: the header must consume the whole line. The parsed
// path is validated but not returned; consumers needing it call
// TokenizeHeader directly.
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

// extractKindName extracts the kind name from a possibly-incomplete
// header string. A header that tokenizes yields its tokenized kind; an
// incomplete or malformed header falls back to its first name token.
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
