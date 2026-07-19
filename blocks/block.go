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

const TheoryOfDualBlockFormats = `
The parser supports two boundary-delimited block formats simultaneously. The
original format places the boundary before the kind as an XML opening tag:
:::<boundary> <kind attr=".."> ... :::<boundary> </kind>. The newer format
places the kind before the boundary and uses a separate metadata line:
:::<kind> <boundary> ... :::end <boundary>. For change blocks in the newer
format, a self-closing XML tag on the line after the opening marker carries
the operation attributes (op, target, file-path). The parser distinguishes the
two formats by checking whether the opening line starts with Han characters
(old format: boundary first) or a non-Han kind word (new format: kind first).
Closing markers are format-specific: old format uses :::<boundary> </kind>,
new format uses :::end <boundary>. Both formats permit trailing non-Han
content after the boundary on the opening and closing lines; the boundary is
always the leading Han ideographs, and extra content is ignored.
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

// parseFirstBlock is the core block parser. When final is false (streaming),
// an unclosed block returns a BlockParseError so the caller can wait for more
// output. When final is true (e.g., at Flush), an unclosed block is treated as
// ended: the block is returned as complete with the body being all remaining
// content after the opening line, and end is set to len(content) so the
// buffer is fully consumed. This prevents post-flush content from combining
// with pre-flush content within the same block. See TheoryOfParserState and
// TheoryOfBoundaryUniqueness.
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

		// Try old format: :::<boundary> <kind ...> (possibly with extra
		// content between boundary and XML tag). See TheoryOfDualBlockFormats.
		if r, matched := tryParseOldFormat(content, openingLine, lineEnd, blockStart, final); matched {
			return r.block, r.start, r.end, r.ok, r.err
		}

		// Try new format: :::<kind> <boundary> with optional metadata
		// line for change blocks. See TheoryOfDualBlockFormats.
		if r, matched := tryParseNewFormat(content, openingLine, lineEnd, blockStart, final); matched {
			return r.block, r.start, r.end, r.ok, r.err
		}

		searchFrom = idx + 1
	}
}

// tryParseOldFormat attempts to parse an old format opening line where the
// boundary (leading Han ideographs) precedes an XML opening tag. Trailing
// non-Han content between the boundary and the XML tag (e.g., "extra") is
// skipped by searching for the first '<' in the rest of the line. Closing
// markers (:::<boundary> </kind>) are rejected. Returns matched=false when
// the line does not conform to the old format. See TheoryOfDualBlockFormats.
func tryParseOldFormat(content []byte, openingLine string, lineEnd, blockStart int, final bool) (result blockParseResult, matched bool) {
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
	bodyEnd, blockEnd, found := findOldFormatClosingMarker(content, bodyStart, boundary, kind)
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
	result.err = &BlockParseError{BlockKind: kind, Boundary: boundary}
	return
}

// tryParseNewFormat attempts to parse a new format opening line where a
// non-Han kind word precedes the boundary (e.g., "change 徕珑"). For change
// blocks, a self-closing XML tag on the next line carries the operation
// attributes. The closing marker is :::end <boundary>. Returns matched=false
// when the line does not conform to the new format or when the kind is "end"
// (a closing marker, not an opening marker). See TheoryOfDualBlockFormats.
func tryParseNewFormat(content []byte, openingLine string, lineEnd, blockStart int, final bool) (result blockParseResult, matched bool) {
	kind, boundary, ok := extractKindAndBoundary(openingLine)
	if !ok || kind == "end" {
		return
	}
	matched = true
	result.block.Kind = kind
	result.block.Boundary = boundary
	bodyStart := lineEnd + 1
	// For change blocks, a self-closing XML tag on the next line carries
	// the operation attributes. See TheoryOfDualBlockFormats.
	if kind == "change" {
		bodyStart = parseNewFormatMetadata(content, bodyStart, &result.block, kind)
	}
	bodyEnd, blockEnd, found := findNewFormatClosingMarker(content, bodyStart, boundary)
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
	result.err = &BlockParseError{BlockKind: kind, Boundary: boundary}
	return
}

// extractKindAndBoundary extracts a non-Han kind word and a Han boundary from
// a new format opening line (e.g., "change 徕珑" -> kind="change",
// boundary="徕珑"). The kind is the leading non-Han, non-whitespace word; the
// boundary is the leading Han ideographs after the kind. Returns ok=false if
// either part is missing. See TheoryOfDualBlockFormats.
func extractKindAndBoundary(s string) (kind, boundary string, ok bool) {
	s = strings.TrimSpace(s)
	var kindBuf []rune
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			break
		}
		if r == ' ' || r == '\t' {
			break
		}
		kindBuf = append(kindBuf, r)
	}
	if len(kindBuf) == 0 {
		return "", "", false
	}
	kind = string(kindBuf)
	rest := strings.TrimSpace(s[len(kind):])
	boundary = extractHanBoundary(rest)
	if boundary == "" {
		return "", "", false
	}
	return kind, boundary, true
}

// findOldFormatClosingMarker searches for :::<boundary> ... </kind> at line
// start, where ... is optional trailing content between the boundary and the
// closing tag. A line-start :::<boundary> with a different boundary is treated
// as body content. See TheoryOfDualBlockFormats and TheoryOfBoundaryUniqueness.
func findOldFormatClosingMarker(content []byte, bodyStart int, boundary, kind string) (bodyEnd, blockEnd int, found bool) {
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

// findNewFormatClosingMarker searches for :::end <boundary> at line start.
// A line-start :::end with a different boundary is treated as body content.
// The boundary is extracted as leading Han ideographs from the text after
// "end", so trailing content after the boundary is ignored. See
// TheoryOfDualBlockFormats and TheoryOfBoundaryUniqueness.
func findNewFormatClosingMarker(content []byte, bodyStart int, boundary string) (bodyEnd, blockEnd int, found bool) {
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
		trimmedLine := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmedLine, "end") {
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom = lineStart + lineEnd + 1
			continue
		}
		rest := strings.TrimSpace(trimmedLine[3:])
		lineBoundary := extractHanBoundary(rest)
		if lineBoundary != boundary {
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom = lineStart + lineEnd + 1
			continue
		}
		bodyEnd = candidate
		if lineEnd == -1 {
			blockEnd = len(content)
		} else {
			blockEnd = lineStart + lineEnd + 1
		}
		return bodyEnd, blockEnd, true
	}
}

// parseNewFormatMetadata checks if the line after the opening marker contains
// a self-closing XML tag with the same kind. If so, it parses the attributes
// into block.Attributes and returns the body start after the metadata line.
// Otherwise, it returns bodyStart unchanged. See TheoryOfDualBlockFormats.
func parseNewFormatMetadata(content []byte, bodyStart int, block *Block, kind string) int {
	if bodyStart >= len(content) {
		return bodyStart
	}
	nextLineEnd := bytes.IndexByte(content[bodyStart:], '\n')
	var nextLine string
	if nextLineEnd == -1 {
		nextLine = string(content[bodyStart:])
	} else {
		nextLine = string(content[bodyStart : bodyStart+nextLineEnd])
	}
	nextLineTrimmed := strings.TrimSpace(nextLine)
	if !strings.HasPrefix(nextLineTrimmed, "<") || strings.HasPrefix(nextLineTrimmed, "</") {
		return bodyStart
	}
	metaKind, metaAttrs, metaValid := parseXMLOpeningTag(nextLineTrimmed)
	if !metaValid || metaKind != kind {
		return bodyStart
	}
	block.Attributes = metaAttrs
	if nextLineEnd == -1 {
		return len(content)
	}
	return bodyStart + nextLineEnd + 1
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
