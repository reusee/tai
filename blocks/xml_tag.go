package blocks

import (
	"encoding/xml"
	"strings"
)

// parseXMLOpeningTag parses an XML opening tag like <kind attr1="val1" attr2="val2">
// and returns the element name (kind) and a map of attributes. Self-closing tags
// (e.g., <change op="DELETE" />) are also accepted; the trailing / is stripped
// before parsing. Returns ok=false if the string is not a valid XML opening tag.
func parseXMLOpeningTag(s string) (kind string, attrs map[string]string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return "", nil, false
	}
	end := strings.Index(s, ">")
	if end == -1 {
		return "", nil, false
	}
	tagContent := s[1:end]
	// Reject closing tags (</kind>).
	if strings.HasPrefix(tagContent, "/") {
		return "", nil, false
	}
	// Trim trailing / for self-closing tags.
	tagContent = strings.TrimSpace(tagContent)
	tagContent = strings.TrimSuffix(tagContent, "/")
	tagContent = strings.TrimSpace(tagContent)

	decoder := xml.NewDecoder(strings.NewReader(s))
	token, err := decoder.Token()
	if err != nil {
		return "", nil, false
	}
	startElement, isStart := token.(xml.StartElement)
	if !isStart {
		return "", nil, false
	}
	attrs = make(map[string]string)
	for _, attr := range startElement.Attr {
		attrs[attr.Name.Local] = attr.Value
	}
	return startElement.Name.Local, attrs, true
}

// parseXMLClosingTag parses an XML closing tag like </kind> and returns the
// element name. Trailing content after the > is ignored. Returns ok=false
// if the string is not a valid XML closing tag.
func parseXMLClosingTag(s string) (kind string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "</") {
		return "", false
	}
	end := strings.Index(s, ">")
	if end == -1 {
		return "", false
	}
	kind = strings.TrimSpace(s[2:end])
	if kind == "" {
		return "", false
	}
	return kind, true
}
