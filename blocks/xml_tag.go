package blocks

import (
	"strings"
)

const TheoryOfXMLTokenizing = `
Streaming XML tag tokenization parses XML opening, closing, and self-closing
tags from potentially incomplete input. The tokenizer tracks quote state
(single and double quotes) so that '>' characters inside attribute values do
not prematurely terminate the tag. A tag is complete only when the
terminating '>' is found outside any quoted value; otherwise the input is
incomplete and the caller must wait for more data before retrying. This is
essential for streaming block headers where attribute values may contain '>'
(e.g., file paths or descriptions with comparison operators). XML entities
(&amp;, &lt;, &gt;, &quot;, &apos;) in attribute values are unescaped to
produce the final value. The tokenizer performs structural parsing only; it
does not validate against any XML schema or DTD.
`

// xmlTagKind enumerates the types of XML tags the tokenizer can produce.
type xmlTagKind int

const (
	xmlTagOpening     xmlTagKind = iota // <kind ...>
	xmlTagClosing                       // </kind>
	xmlTagSelfClosing                   // <kind ... />
)

// XMLTagToken represents a parsed XML tag produced by the streaming tokenizer.
type XMLTagToken struct {
	Kind       string
	Attributes map[string]string
	tagKind    xmlTagKind
}

// IsClosing reports whether the token is a closing tag (</kind>).
func (t XMLTagToken) IsClosing() bool {
	return t.tagKind == xmlTagClosing
}

// IsSelfClosing reports whether the token is a self-closing tag (<kind ... />).
func (t XMLTagToken) IsSelfClosing() bool {
	return t.tagKind == xmlTagSelfClosing
}

// IsOpening reports whether the token is an opening tag (<kind ...>) that is
// not self-closing.
func (t XMLTagToken) IsOpening() bool {
	return t.tagKind == xmlTagOpening
}

// isXMLSpace reports whether b is an XML whitespace character (space, tab,
// newline, carriage return) per the XML 1.0 specification.
func isXMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isXMLNameChar reports whether b is a valid XML name character. XML names
// can contain letters, digits, hyphens, underscores, periods, and colons.
func isXMLNameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '_' || b == '.' || b == ':'
}

// TokenizeXMLTag parses a single XML tag from the input string. It correctly
// handles '>' inside quoted attribute values and detects incomplete tags for
// streaming input. Returns the parsed token, the number of bytes consumed
// (including the terminating '>' or '/>'), and whether a complete tag was
// found.
//
// When ok is false, the tag is incomplete or malformed; the caller should
// buffer more input and retry (for streaming) or skip the input (for a
// complete line that does not contain a valid tag). See TheoryOfXMLTokenizing.
func TokenizeXMLTag(s string) (token XMLTagToken, consumed int, ok bool) {
	pos := 0
	n := len(s)

	// Skip leading whitespace.
	for pos < n && isXMLSpace(s[pos]) {
		pos++
	}
	if pos >= n {
		return XMLTagToken{}, 0, false
	}
	if s[pos] != '<' {
		return XMLTagToken{}, 0, false
	}
	pos++ // skip '<'

	// Check for closing tag (</kind>).
	if pos < n && s[pos] == '/' {
		return tokenizeXMLClosingTag(s, pos)
	}

	return tokenizeXMLOpeningTag(s, pos)
}

// tokenizeXMLClosingTag parses a closing tag starting after '</'. It reads
// the element name, skips whitespace, and expects '>'. Closing tags have no
// attributes. Content between the name and '>' must be whitespace only; any
// other character causes the tag to be rejected. See TheoryOfXMLTokenizing.
func tokenizeXMLClosingTag(s string, pos int) (token XMLTagToken, consumed int, ok bool) {
	n := len(s)
	pos++ // skip '/'

	// Read element name.
	nameStart := pos
	for pos < n && isXMLNameChar(s[pos]) {
		pos++
	}
	name := s[nameStart:pos]
	if name == "" {
		return XMLTagToken{}, 0, false
	}

	// Skip whitespace before '>'.
	for pos < n && isXMLSpace(s[pos]) {
		pos++
	}
	if pos >= n {
		return XMLTagToken{}, 0, false
	}
	if s[pos] != '>' {
		return XMLTagToken{}, 0, false
	}
	pos++ // skip '>'

	return XMLTagToken{
		Kind:    name,
		tagKind: xmlTagClosing,
	}, pos, true
}

// tokenizeXMLOpeningTag parses an opening or self-closing tag starting after
// '<'. It reads the element name, attributes (name="value" pairs), and the
// terminating '>' or '/>'. The scanner tracks quote state so '>' inside
// attribute values does not terminate the tag. See TheoryOfXMLTokenizing.
func tokenizeXMLOpeningTag(s string, pos int) (token XMLTagToken, consumed int, ok bool) {
	n := len(s)

	// Read element name.
	nameStart := pos
	for pos < n && isXMLNameChar(s[pos]) {
		pos++
	}
	name := s[nameStart:pos]
	if name == "" {
		return XMLTagToken{}, 0, false
	}

	attrs := map[string]string{}

	for {
		// Skip whitespace between attributes.
		for pos < n && isXMLSpace(s[pos]) {
			pos++
		}
		if pos >= n {
			return XMLTagToken{}, 0, false
		}

		// Check for end of opening tag.
		if s[pos] == '>' {
			pos++ // skip '>'
			return XMLTagToken{
				Kind:       name,
				Attributes: attrs,
				tagKind:    xmlTagOpening,
			}, pos, true
		}

		// Check for self-closing tag (/ >).
		if s[pos] == '/' {
			pos++ // skip '/'
			// Skip whitespace before '>'.
			for pos < n && isXMLSpace(s[pos]) {
				pos++
			}
			if pos >= n {
				return XMLTagToken{}, 0, false
			}
			if s[pos] != '>' {
				return XMLTagToken{}, 0, false
			}
			pos++ // skip '>'
			return XMLTagToken{
				Kind:       name,
				Attributes: attrs,
				tagKind:    xmlTagSelfClosing,
			}, pos, true
		}

		// Parse attribute name.
		attrNameStart := pos
		for pos < n && isXMLNameChar(s[pos]) {
			pos++
		}
		attrName := s[attrNameStart:pos]
		if attrName == "" {
			return XMLTagToken{}, 0, false
		}

		// Skip whitespace before '='.
		for pos < n && isXMLSpace(s[pos]) {
			pos++
		}
		if pos >= n {
			return XMLTagToken{}, 0, false
		}
		if s[pos] != '=' {
			return XMLTagToken{}, 0, false
		}
		pos++ // skip '='

		// Skip whitespace before value.
		for pos < n && isXMLSpace(s[pos]) {
			pos++
		}
		if pos >= n {
			return XMLTagToken{}, 0, false
		}

		// Parse attribute value (must be quoted).
		if s[pos] != '"' && s[pos] != '\'' {
			return XMLTagToken{}, 0, false
		}
		quote := s[pos]
		pos++ // skip opening quote
		valueStart := pos
		// Scan until the matching closing quote. Characters inside the
		// quote — including '>' — are part of the value and do not
		// terminate the tag. See TheoryOfXMLTokenizing.
		for pos < n && s[pos] != quote {
			pos++
		}
		if pos >= n {
			return XMLTagToken{}, 0, false // unclosed quote
		}
		rawValue := s[valueStart:pos]
		pos++ // skip closing quote

		attrs[attrName] = unescapeXMLAttrValue(rawValue)
	}
}

// unescapeXMLAttrValue unescapes the five predefined XML entities in an
// attribute value: &amp; → &, &lt; → <, &gt; → >, &quot; → ", &apos; → '.
// Unknown entities (not terminated by ';' or not recognized) are left as-is.
// See TheoryOfXMLTokenizing.
func unescapeXMLAttrValue(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var buf strings.Builder
	buf.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] != '&' {
			buf.WriteByte(s[i])
			i++
			continue
		}
		switch {
		case strings.HasPrefix(s[i:], "&amp;"):
			buf.WriteByte('&')
			i += 5
		case strings.HasPrefix(s[i:], "&lt;"):
			buf.WriteByte('<')
			i += 4
		case strings.HasPrefix(s[i:], "&gt;"):
			buf.WriteByte('>')
			i += 4
		case strings.HasPrefix(s[i:], "&quot;"):
			buf.WriteByte('"')
			i += 6
		case strings.HasPrefix(s[i:], "&apos;"):
			buf.WriteByte('\'')
			i += 6
		default:
			buf.WriteByte('&')
			i++
		}
	}
	return buf.String()
}

// parseXMLOpeningTag parses an XML opening tag (including self-closing tags)
// and returns the element name and attributes. Returns ok=false if the input
// is not a valid or complete opening tag. Closing tags (</kind>) are rejected.
// See TheoryOfXMLTokenizing.
func parseXMLOpeningTag(s string) (kind string, attrs map[string]string, ok bool) {
	token, _, ok2 := TokenizeXMLTag(s)
	if !ok2 {
		return "", nil, false
	}
	if token.IsClosing() {
		return "", nil, false
	}
	return token.Kind, token.Attributes, true
}

// parseXMLClosingTag parses an XML closing tag (</kind>) and returns the
// element name. Content between the element name and '>' must be whitespace
// only; any other character causes the tag to be rejected. Trailing content
// after '>' is ignored. Returns ok=false if the input is not a valid or
// complete closing tag. Opening and self-closing tags are rejected.
// See TheoryOfXMLTokenizing.
func parseXMLClosingTag(s string) (kind string, ok bool) {
	token, _, ok2 := TokenizeXMLTag(s)
	if !ok2 {
		return "", false
	}
	if !token.IsClosing() {
		return "", false
	}
	return token.Kind, true
}
