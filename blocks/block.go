package blocks

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
)

const BlockFormatTheory = `
The boundary block format is a general-purpose structured output format for AI models.
It uses delimited blocks with a random boundary string to avoid parsing conflicts with content.
Each block has a kind (XML element name), attributes (XML attributes on the opening tag), and a body.
The opening marker is :::<boundary> <kind attr1=".." attr2="..."> and the closing marker is
:::<boundary> </kind>. Only the boundary string and the XML element structure are unified;
the body content is defined by the specific kind.
This format replaces ad-hoc XML or JSON escaping with a simple, parseable structure.

**Line-start requirement**: The opening marker and the closing marker must each appear at the
beginning of a line. Any occurrence of ":::" that is not at the start of a line is treated as
regular content and will not start a block. Models tend to glue the opening marker to the end
of a preceding prose line rather than starting it on its own line, which causes the block to
be silently ignored. The system prompt must therefore emphasize this rule with explicit
correct/incorrect examples so the model internalizes the newline-before-marker discipline.

**No surrounding blank lines**: Blocks do not require blank lines before or after them.
A block can appear directly adjacent to other text or other blocks; the only structural
requirement is that each marker starts at the beginning of a line.

**Unclosed block detection**: An opening marker at line start without a matching closing
marker is a malformed block. The parser reports an error rather than silently skipping it,
ensuring that incomplete output from the AI is surfaced to the user.
`

const TheoryOfBoundaryUniqueness = `
The boundary string is the sole disambiguator between consecutive delimited blocks within
a single response. The parser closes a block at the first :::<boundary> </kind> marker found
at line start whose boundary matches the opening marker's boundary and whose closing tag
matches the opening kind. A line-start :::<boundary> with a different boundary is treated
as body content, not a closing marker, because the body may legitimately contain example
markers or other text that starts with ":::". The parser does not report a mismatched-
boundary error; it simply continues scanning for the matching boundary. If no matching
:::<boundary> </kind> is found, the block is unclosed. The closing marker line does not
require a trailing newline: when the end marker is the last content in the buffer, the
boundary and closing tag are extracted from the remaining content. If the boundary is
incomplete (still streaming), the shorter extracted string will not match, so the block
remains unclosed until the full boundary arrives.

Therefore the boundary must be a freshly generated random pair of uncommon, meaningless
Chinese characters, never copied from the illustrative examples. The example blocks in the
system prompt deliberately use distinct boundaries to demonstrate this rule, and those exact
strings are forbidden for reuse. The randomness of the boundary is the integrity guarantee of
the format: if the model reuses an example boundary, a subsequent real block opened with that
same boundary would close at the wrong marker.

The boundary characters must also be disjoint from the block body. Because the parser closes
the block at the first line-start :::<boundary> whose boundary matches, a body line that
begins with ":::" followed by the same two ideographs and a matching closing tag would
prematurely terminate the block and discard all remaining content. Block bodies are
predominantly source code (ASCII), so most Han characters never occur in them; the collision
risk arises when the body contains Chinese comments, string literals, or documentation that
reproduces block markers. The model should therefore select rare, uncommon ideographs that
do not appear anywhere in the code or text it is about to emit, satisfying the anti-reuse
guarantee and the body-disjointness guarantee simultaneously.
`

const TheoryOfBlockFormat = `
The parser uses a single boundary-delimited block format. The boundary (a
random string of two uncommon, meaningless Chinese characters) precedes the
kind as an XML opening tag:
:::<boundary> <kind attr=".."> ... :::<boundary> </kind>. The boundary is
extracted as the leading Han ideographs from the opening and closing lines;
trailing non-Han content after the boundary is ignored. Closing markers
(:::<boundary> </kind>) are always rejected as opening markers. The boundary
string is the sole disambiguator between consecutive blocks within a single
response.
`

// blockParseResult holds the outcome of attempting to parse a block in one
// format. When matched is true, the format was recognized and the result
// fields are populated; when false, the format did not apply and the caller
// should try the next format.
type blockParseResult struct {
	block Block
	start int
	end   int
	ok    bool
	err   error
}

const BlockFormatSystemPrompt = `**Structured Output Format (Boundary-Delimited):**

Your response can include structured content using delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
:::<boundary> <kind attr1=".." attr2="...">
<kind-specific content>
:::<boundary> </kind>

- <boundary>: A random string composed of two uncommon meaningless Chinese characters that do not appear in the block body. A sufficiently random boundary ensures it cannot conflict with any content. Use a different boundary for each block in the same response. The same boundary MUST be used for the start and end markers of a single block.
- <kind>: The type of block, specified as an XML element name. The valid kinds and their content formats are defined by the specific kind documentation. Attributes on the opening tag provide kind-specific metadata.
- Content: The body between the start and end markers is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but every marker must start at the beginning of its own line.
- If no blocks are needed, simply omit them.

**Line-Start Requirement (CRITICAL):**
- The opening marker (:::<boundary> <kind ...>) and the closing marker (:::<boundary> </kind>) MUST each appear at the beginning of a line — immediately after a newline character or at the very start of the response.
- NEVER place a marker at the end of a line of text. If you have prose immediately before a block, end the prose with a newline first, then start the marker on its own new line.
- Any ":::" that is not at the start of a line is treated as regular content and will NOT be recognized as a block marker; the block will be silently ignored and your changes will be lost.
- Do this (marker starts on its own line after the prose):
  Some explanation text.
  :::徕珑 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  :::徕珑 </change>
- NOT this (marker glued to the end of the prose line — the block will NOT be parsed and your changes will be lost):
  Some explanation text.:::徕珑 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  :::徕珑 </change>

**Boundary Uniqueness (CRITICAL):**
- Generate a fresh random pair of two uncommon, meaningless Chinese characters as the boundary for each block.
- **Never reuse a boundary string that appears in any example in this prompt.** The example boundaries are illustrative only; copying them causes the parser to mismatch closing markers and corrupt blocks.
- Each block in a response must use a distinct boundary so the parser can unambiguously pair each opening marker with its closing marker.
- **Avoid body-content characters**: Select boundary characters that do not appear anywhere in the block body (the code or text between the markers). A body line that starts with ":::" followed by the same boundary prematurely closes the block and truncates the remaining content. Since block bodies are predominantly source code (ASCII), most Han characters are safe; pick rare, uncommon ideographs absent from any Chinese comments, string literals, or documentation you are about to emit.

**Boundary Matching (CRITICAL):**
- The closing marker MUST use the EXACT same boundary string as the opening marker. A block opened with :::徕珑 <change ...> MUST be closed with :::徕珑 </change>, never :::栢彣 </change> or any other boundary.
- A line-start :::<boundary> with a different boundary is treated as body content, not a closing marker. The parser continues scanning for the matching boundary. If no matching closing marker is found, the block is unclosed. Always close a block with the same boundary you opened it with.
- Before writing each closing marker, verify its boundary matches the corresponding opening marker of the same block. The most common cause of mismatched boundaries is copying a boundary from another block or from an example instead of reusing the one you opened with.
`

// Block represents a parsed boundary block.
type Block struct {
	Kind       string
	Boundary   string
	Attributes map[string]string
	Body       string
}

// ParseFirstBlock parses the first complete boundary block from content.
// An unclosed block (opening marker with no matching end marker at line
// start) returns a BlockParseError. During streaming, this indicates
// incomplete output that may be completed by subsequent chunks. Use
// parseFirstBlock with final=true to finalize unclosed blocks at Flush.
func ParseFirstBlock(content []byte) (block Block, start int, end int, ok bool, err error) {
	return parseFirstBlock(content, false)
}

func parseFirstBlock(content []byte, final bool) (block Block, start int, end int, ok bool, err error) {
	searchFrom := 0
	for {
		idx := bytes.Index(content[searchFrom:], []byte(":::"))
		if idx == -1 {
			return
		}
		idx += searchFrom

		// The opening marker must be at the beginning of a line.
		if idx > 0 && content[idx-1] != '\n' {
			searchFrom = idx + 3
			continue
		}
		blockStart := idx

		// Extract the opening line after :::
		lineStart := idx + 3
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		if lineEnd == -1 {
			searchFrom = idx + 1
			continue
		}
		lineEnd += lineStart
		openingLine := string(content[lineStart:lineEnd])

		// Parse the block in the boundary-delimited format:
		// :::<boundary> <kind ...> ... :::<boundary> </kind>
		// See TheoryOfBlockFormat.
		if r, matched := tryParseBlock(content, openingLine, lineEnd, blockStart, final); matched {
			return r.block, r.start, r.end, r.ok, r.err
		}

		searchFrom = idx + 1
	}
}

// tryParseBlock attempts to parse an opening line where the boundary (leading
// Han ideographs) precedes an XML opening tag. Trailing non-Han content
// between the boundary and the XML tag (e.g., "extra") is skipped by searching
// for the first '<' in the rest of the line. Closing markers
// (:::<boundary> </kind>) are rejected. Returns matched=false when the line
// does not conform to the format. See TheoryOfBlockFormat.
func tryParseBlock(content []byte, openingLine string, lineEnd, blockStart int, final bool) (result blockParseResult, matched bool) {
	boundary := extractHanBoundary(openingLine)
	if boundary == "" {
		return
	}
	rest := openingLine[len(boundary):]
	ltIdx := strings.Index(rest, "<")
	if ltIdx == -1 {
		return
	}
	xmlPart := strings.TrimSpace(rest[ltIdx:])
	// :::<boundary> </kind> is a closing marker, never an opening marker.
	if strings.HasPrefix(xmlPart, "</") {
		return
	}
	kind, attrs, valid := parseXMLOpeningTag(xmlPart)
	if !valid || kind == "" {
		return
	}
	matched = true
	result.block.Kind = kind
	result.block.Boundary = boundary
	result.block.Attributes = attrs
	bodyStart := lineEnd + 1
	bodyEnd, blockEnd, found := findClosingMarker(content, bodyStart, boundary, kind)
	if found {
		result.block.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
		result.start = blockStart
		result.end = blockEnd
		result.ok = true
		return
	}
	if final {
		result.block.Body = strings.TrimSpace(string(content[bodyStart:]))
		result.start = blockStart
		result.end = len(content)
		result.ok = true
		return
	}
	// Set start and end even in the error path so callers (e.g.,
	// parseMemoryItems) can skip past the unclosed block's opening
	// marker and continue scanning for subsequent blocks.
	result.start = blockStart
	result.end = lineEnd + 1
	result.err = &BlockParseError{BlockKind: kind, Boundary: boundary}
	return
}

// findClosingMarker searches for :::<boundary> ... </kind> at line
// start, where ... is optional trailing content between the boundary and the
// closing tag. A line-start :::<boundary> with a different boundary is treated
// as body content. See TheoryOfBoundaryUniqueness.
func findClosingMarker(content []byte, bodyStart int, boundary, kind string) (bodyEnd, blockEnd int, found bool) {
	searchFrom := bodyStart
	for {
		offset := bytes.Index(content[searchFrom:], []byte(":::"))
		if offset == -1 {
			return 0, 0, false
		}
		candidate := searchFrom + offset
		if candidate > 0 && content[candidate-1] != '\n' {
			searchFrom = candidate + 3
			continue
		}
		lineStart := candidate + 3
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		var line string
		if lineEnd == -1 {
			line = string(content[lineStart:])
		} else {
			line = string(content[lineStart : lineStart+lineEnd])
		}
		lineBoundary := extractHanBoundary(line)
		if lineBoundary != boundary {
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom = lineStart + lineEnd + 1
			continue
		}
		rest := line[len(boundary):]
		closeIdx := strings.Index(rest, "</")
		if closeIdx == -1 {
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom = lineStart + lineEnd + 1
			continue
		}
		closePart := strings.TrimSpace(rest[closeIdx:])
		endKind, isClosing := parseXMLClosingTag(closePart)
		if isClosing && endKind == kind {
			bodyEnd = candidate
			if lineEnd == -1 {
				blockEnd = len(content)
			} else {
				blockEnd = lineStart + lineEnd + 1
			}
			return bodyEnd, blockEnd, true
		}
		if lineEnd == -1 {
			return 0, 0, false
		}
		searchFrom = lineStart + lineEnd + 1
	}
}

// extractHanBoundary extracts the leading Han (Chinese) ideographs from s.
// Leading and trailing whitespace are trimmed first. Parsing then collects
// consecutive Han characters and stops at the first non-Han character, which
// is ignored and terminates the boundary. This ensures trailing content after
// the boundary string (e.g., model-added annotations) does not corrupt block
// matching. A field with no Han characters yields an empty string, causing the
// marker to be skipped. See TheoryOfBoundaryUniqueness.
func extractHanBoundary(s string) string {
	s = strings.TrimSpace(s)
	var buf []rune
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			buf = append(buf, r)
		} else {
			break
		}
	}
	return string(buf)
}

// BlockParseError is returned by ParseFirstBlock for unclosed boundary blocks.
// An unclosed block is an opening marker with no matching :::end <boundary>
// marker at line start. During streaming this may indicate incomplete output
// rather than a definitive error. See TheoryOfBoundaryUniqueness.
type BlockParseError struct {
	BlockKind string
	Boundary  string
}

func (e *BlockParseError) Error() string {
	return fmt.Sprintf("unclosed block: kind %q boundary %q has no matching end marker", e.BlockKind, e.Boundary)
}
