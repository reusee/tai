package blocks

import (
	"bytes"
	"fmt"
	"strings"
)

const TheoryOfBlockFormatGeneral = `
The heredoc block format is a general-purpose structured output format for AI models.
It uses heredoc-style delimiters with an XML opening tag to avoid parsing conflicts with content.
Each block has a kind (XML element name), attributes (XML attributes on the opening tag), and a body.
The opening marker is <<DELIMITER <kind attr1=".." attr2="..."> and the closing marker is
DELIMITER on its own line. The delimiter is a pair of uncommon Chinese characters chosen by the
model; the closing line is simply the delimiter alone, with no XML closing tag. Only the delimiter
string and the XML element structure are unified; the body content is defined by the specific kind.
This format leverages the familiar heredoc syntax that models already know, improving
parsing reliability.

**Delimiter selection policy**: Every delimiter MUST be exactly two uncommon Chinese characters
(for example 徕珑). Content — source code, prompts, and configuration text — is overwhelmingly
ASCII or common-script writing, so a pair of rare Chinese characters has negligible probability
of appearing in a block body, satisfying the body-disjointness guarantee without requiring the
model to scan its output for collisions. The fixed two-character length makes the delimiter
visually distinct from content and keeps token cost constant. The model must never emit the
literal placeholder "DELIMITER", must never reuse an example delimiter, and must never use a
delimiter of any other length or script.

**Line-start requirement**: The opening marker must appear at the beginning of a line.
The closing marker (the delimiter alone) must also appear on its own line. Any ` + "`<<`" + ` that is
not at the start of a line is treated as regular content and will not start a block. Models tend
to glue the opening marker to the end of a preceding prose line rather than starting it on its
own line, which causes the block to be silently ignored. The system prompt must therefore
emphasize this rule with explicit correct/incorrect examples so the model internalizes the
newline-before-marker discipline.

**No surrounding blank lines**: Blocks do not require blank lines before or after them.
A block can appear directly adjacent to other text or other blocks; the only structural
requirement is that the opening marker starts at the beginning of a line and the closing
marker is on its own line.

**Unclosed block detection**: An opening marker at line start without a matching closing
line (the delimiter alone on its own line) is a malformed block. The parser reports an error
rather than silently skipping it, ensuring that incomplete output from the AI is surfaced
to the user.

**Delimiter rule centralization**: The delimiter rules (selection, uniqueness,
body-disjointness, matching) are centralized in BlockFormatSystemPrompt and
BlockFormatRestatePrompt. Individual block kind prompts (continue, shell, go-test,
summary, memory) must not restate the full rule set; they reference the general format
and focus on their kind-specific semantics. Kind prompts do display structurally
complete examples with illustrative concrete delimiters, because showing the literal
placeholder marker "<<DELIMITER" inside a kind template teaches the model to emit
that placeholder verbatim, producing blocks with a non-unique delimiter. Each kind
prompt may carry one pointed reminder tied to its example — choose a fresh pair of
uncommon Chinese characters, repeat the same delimiter on the closing line, never write
the placeholder literally — without restating the full rules. This keeps redundant
delimiter description near zero while giving the format-error-prone kinds (notably
go-test and summary) correct imitation targets for both their opening and closing
markers.
`

const TheoryOfBoundaryUniqueness = `
The delimiter is the sole disambiguator between consecutive delimited blocks within
a single response. The parser closes a block at the first line that exactly matches
the opening marker's delimiter. A line that does not match the delimiter is treated as
body content. If no matching delimiter line is found, the block is unclosed. The
closing marker line does not require a trailing newline: when the delimiter is the
last content in the buffer, the parser extracts it from the remaining content. If
the delimiter is incomplete (still streaming), the shorter extracted string will not
match, so the block remains unclosed until the full delimiter arrives.

Therefore the delimiter must be freshly chosen as exactly two uncommon Chinese
characters, never copied from the illustrative examples. The example blocks in the
system prompt deliberately use distinct delimiters to demonstrate this rule, and
those exact strings are forbidden for reuse. The rarity of the chosen characters is
the integrity guarantee of the format: a pair of uncommon Chinese characters is
effectively absent from code and prose, so the chance of a body line accidentally
matching the delimiter is negligible, while reusing an example delimiter would cause
a subsequent real block opened with that same delimiter to close at the wrong marker.

The delimiter must also be disjoint from the block body; this is a hard requirement,
not an optional suggestion. Because the parser closes the block at the first line
matching the delimiter, a body line that matches the delimiter would prematurely
terminate the block and discard all remaining content. The model must therefore
select a delimiter that does not appear anywhere in the code or text it is about
to emit. Two uncommon Chinese characters satisfy this by construction for code and
prose content, but the model must verify the chosen pair is absent from the body
before emitting the block. Body-disjointness is as important as the anti-reuse
guarantee: both are integrity guarantees of the format, and violating either
corrupts the block.

Nested block parsing (see TheoryOfNestedBlockParsing) provides a safety net when
the body unavoidably contains the delimiter as part of a nested block marker. The
stack-based closing-marker scanner tracks nesting depth, so an inner block's
closing marker pops the inner level rather than prematurely closing the outer
block. This does not relax the body-disjointness recommendation for general content,
but it correctly handles the specific case where the body contains well-formed
nested blocks.
`

const TheoryOfNestedBlockParsing = `
The parser supports nested blocks: when a block body contains another block
opening marker (<<DELIMITER <kind ...>), the inner block's closing marker does
not prematurely close the outer block. The closing-marker scanner maintains a
delimiter stack initialized with the outer block's delimiter. Each line that
starts with "<<" and contains a valid XML opening tag after the delimiter
pushes that delimiter onto the stack, marking the start of a nested block. A
line that is a delimiter alone on its own line pops the stack only if it matches
the top; when the stack becomes empty, the outer block is closed. A closing
marker that does not match the top of the stack is treated as body content.

This correctly handles same-delimiter nesting (the inner block's closing marker
pops the inner level, not the outer level) and different-delimiter nesting (a
non-matching delimiter line is body content at the current nesting level). When
a nested block is unclosed, the outer block is also unclosed, because the
stack never returns to empty.

The XML-tag validation after the delimiter prevents false positives from
content that starts with "<<" but is not a block opening. The validation
mirrors tryParseBlock: it extracts the delimiter, finds the first "<" in the
remainder, and calls parseXMLOpeningTag to verify a well-formed XML tag. Lines
like "<<some code" (no XML tag) or "<<text with < angle brackets>" (invalid XML
tag) are treated as body content, not nested openings. This avoids false
nesting from shell heredocs, code comments, or prose that happens to start with
"<<".
`

const TheoryOfBlockFormat = `
The parser uses a heredoc-style block format. The delimiter (a random string)
precedes the kind as an XML opening tag:
<<DELIMITER <kind attr=".."> ... DELIMITER. The delimiter is extracted as the
text between ` + "`<<`" + ` and the first whitespace or ` + "`<`" + ` character on the opening
line; trailing content after the delimiter is skipped by searching for the first
` + "`<`" + ` in the rest of the line. The closing marker is the delimiter alone on its own
line — no XML closing tag is needed. The delimiter is the sole disambiguator between
consecutive blocks within a single response.
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

const BlockFormatSystemPrompt = `**Structured Output Format (Heredoc-Delimited):**

Your response can include structured content using heredoc-delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
<<DELIMITER <kind attr1=".." attr2="...">
<kind-specific content>
DELIMITER

- DELIMITER: Exactly two uncommon Chinese characters (e.g., 徕珑) that do not appear in the block body. The rarity of the characters ensures the delimiter cannot conflict with any content. Use a different pair of uncommon Chinese characters for each block in the same response. The same delimiter MUST be used for the start marker and the closing line.
- <kind>: The type of block, specified as an XML element name. The valid kinds and their content formats are defined by the specific kind documentation. Attributes on the opening tag provide kind-specific metadata.
- Content: The body between the start marker and the closing line is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but the opening marker must start at the beginning of its own line and the closing delimiter must be on its own line.
- If no blocks are needed, simply omit them.

**Line-Start Requirement (CRITICAL):**
- The opening marker (<<DELIMITER <kind ...>) MUST appear at the beginning of a line — immediately after a newline character or at the very start of the response.
- The closing marker (DELIMITER) MUST appear on its own line — the delimiter alone, with nothing else on that line.
- NEVER place the opening marker at the end of a line of text. If you have prose immediately before a block, end the prose with a newline first, then start the marker on its own new line.
- Any ` + "`<<`" + ` that is not at the start of a line is treated as regular content and will NOT be recognized as a block marker; the block will be silently ignored and your changes will be lost.
- Do this (marker starts on its own line after the prose):
  Some explanation text.
  <<徕珑 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  徕珑
- NOT this (marker glued to the end of the prose line — the block will NOT be parsed and your changes will be lost):
  Some explanation text.<<徕珑 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  徕珑

**Delimiter Uniqueness (CRITICAL):**
- Generate a fresh delimiter for each block: exactly two uncommon Chinese characters (e.g., 龘靐).
- **Never reuse a delimiter that appears in any example in this prompt.** The example delimiters are illustrative only; copying them causes the parser to mismatch closing markers and corrupt blocks.
- Each block in a response must use a distinct pair of uncommon Chinese characters so the parser can unambiguously pair each opening marker with its closing line.
- **Body-disjointness (HARD REQUIREMENT)**: The delimiter MUST NOT appear anywhere in the block body (the code or text between the markers). Because the parser closes the block at the first line matching the delimiter, a body line that matches the delimiter prematurely closes the block and truncates all remaining content. Two uncommon Chinese characters are very unlikely to appear in code or prose, but you MUST verify the chosen pair is absent from the body before emitting the block. This is not a suggestion: a delimiter that appears in the body corrupts the block.

**Delimiter Matching (CRITICAL):**
- The closing line MUST use the EXACT same delimiter string as the opening marker. A block opened with <<徕珑 <change ...> MUST be closed with 徕珑, never 龘靐 or any other delimiter.
- A line that does not match the delimiter is treated as body content, not a closing marker. The parser continues scanning for the matching delimiter. If no matching closing line is found, the block is unclosed. Always close a block with the same delimiter you opened it with.
- Before writing each closing line, verify its delimiter matches the corresponding opening marker of the same block. The most common cause of mismatched delimiters is copying a delimiter from another block or from an example instead of reusing the one you opened with.
`

const BlockFormatRestatePrompt = `- **Block format (CRITICAL)**: Every block opening marker line MUST start at the beginning of its own line, immediately after a newline. The closing line is the delimiter alone on its own line. NEVER glue the opening marker to the end of a prose line — the block will be silently ignored and your changes will be lost.
- **Header/Footer checklist**: Each block needs TWO markers — never omit either. Opening marker: '<<' followed by your freshly chosen delimiter (exactly two uncommon Chinese characters) and the opening tag '<kind ...>' ending with '>'. Closing marker: the SAME delimiter alone on its own line. Never swap or alter either marker.
- **The DELIMITER MUST be exactly two uncommon Chinese characters** (e.g., 徕珑, 龘靐, 齉爩), NEVER the literal text "<DELIMITER>" or a common word. If you write "<<DELIMITER" literally, the parser cannot recognize the block and your changes will be silently lost.
- Generate a fresh pair of uncommon Chinese characters for each block. Never reuse a delimiter from any example in this prompt.
- The closing line MUST use the EXACT same delimiter as the opening marker.
- **Body-disjointness (HARD REQUIREMENT)**: The delimiter MUST NOT appear anywhere in the block body. This is a hard requirement: a body line matching the delimiter prematurely closes the block and truncates all remaining content. Two uncommon Chinese characters satisfy this by construction for code and prose, but you MUST verify the chosen pair is absent from the body before emitting the block.
- No blank lines are required before or after a block.`

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
// incomplete output that may be completed by subsequent chunks.
func ParseFirstBlock(content []byte) (block Block, start int, end int, ok bool, err error) {
	return parseFirstBlock(content)
}

func parseFirstBlock(content []byte) (block Block, start int, end int, ok bool, err error) {
	searchFrom := 0
	for {
		idx := bytes.Index(content[searchFrom:], []byte("<<"))
		if idx == -1 {
			return
		}
		idx += searchFrom

		// The opening marker must be at the beginning of a line.
		if idx > 0 && content[idx-1] != '\n' {
			searchFrom = idx + 2
			continue
		}
		blockStart := idx

		// Extract the opening line after <<
		lineStart := idx + 2
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		if lineEnd == -1 {
			searchFrom = idx + 1
			continue
		}
		lineEnd += lineStart
		openingLine := string(content[lineStart:lineEnd])

		// Parse the block in the heredoc-delimited format:
		// <<DELIMITER <kind ...> ... DELIMITER
		// See TheoryOfBlockFormat.
		if r, matched := tryParseBlock(content, openingLine, lineEnd, blockStart); matched {
			return r.block, r.start, r.end, r.ok, r.err
		}

		searchFrom = idx + 1
	}
}

func tryParseBlock(content []byte, openingLine string, lineEnd, blockStart int) (result blockParseResult, matched bool) {
	delimiter := extractDelimiter(openingLine)
	if delimiter == "" {
		return
	}
	rest := openingLine[len(delimiter):]
	ltIdx := strings.Index(rest, "<")
	if ltIdx == -1 {
		return
	}
	xmlPart := strings.TrimSpace(rest[ltIdx:])
	// In heredoc format, the closing marker is the delimiter alone,
	// so there is no XML closing tag on the opening line to reject.
	// However, reject if the XML part starts with </ as a safety check.
	if strings.HasPrefix(xmlPart, "</") {
		return
	}
	kind, attrs, valid := parseXMLOpeningTag(xmlPart)
	if !valid || kind == "" {
		return
	}
	matched = true
	result.block.Kind = kind
	result.block.Boundary = delimiter
	result.block.Attributes = attrs
	bodyStart := lineEnd + 1
	bodyEnd, blockEnd, found := findClosingMarker(content, bodyStart, delimiter)
	if found {
		result.block.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
		result.start = blockStart
		result.end = blockEnd
		result.ok = true
		return
	}
	// Unclosed block: no matching end marker found. Always return an
	// error, never finalize. An unclosed block is incomplete regardless
	// of whether Flush has been called. See TheoryOfBlockFormat.
	result.start = blockStart
	result.end = lineEnd + 1
	result.err = &BlockParseError{BlockKind: kind, Boundary: delimiter}
	return
}

// nestedOpeningDelimiter checks if a line starting with "<<" is a valid
// nested block opening marker and returns its delimiter. It validates
// that the delimiter is followed by a valid XML opening tag, mirroring
// the logic in tryParseBlock. This prevents false positives from content
// that starts with "<<" but is not a block opening (e.g., shell heredocs,
// text with angle brackets). See TheoryOfNestedBlockParsing.
func nestedOpeningDelimiter(line string) (delimiter string, ok bool) {
	if !strings.HasPrefix(line, "<<") {
		return "", false
	}
	afterMarker := line[2:]
	delimiter = extractDelimiter(afterMarker)
	if delimiter == "" {
		return "", false
	}
	rest := afterMarker[len(delimiter):]
	ltIdx := strings.Index(rest, "<")
	if ltIdx == -1 {
		return "", false
	}
	xmlPart := strings.TrimSpace(rest[ltIdx:])
	if strings.HasPrefix(xmlPart, "</") {
		return "", false
	}
	kind, _, valid := parseXMLOpeningTag(xmlPart)
	if !valid || kind == "" {
		return "", false
	}
	return delimiter, true
}

// findClosingMarker searches for the delimiter alone on its own line within
// the content starting from bodyStart. It uses a stack-based approach to
// handle nested blocks: lines starting with "<<" that contain a valid XML
// opening tag push their delimiter onto the stack, and a closing marker pops
// the stack only if it matches the top. The outer block closes when the stack
// becomes empty. See TheoryOfNestedBlockParsing.
func findClosingMarker(content []byte, bodyStart int, delimiter string) (bodyEnd, blockEnd int, found bool) {
	stack := []string{delimiter}
	searchFrom := bodyStart
	for {
		lineEnd := bytes.IndexByte(content[searchFrom:], '\n')
		var line string
		if lineEnd == -1 {
			line = string(content[searchFrom:])
		} else {
			line = string(content[searchFrom : searchFrom+lineEnd])
		}

		// Check for a nested opening marker. The XML-tag validation
		// prevents false positives from content that starts with "<<"
		// but is not a block opening. See TheoryOfNestedBlockParsing.
		if nestedDelim, nested := nestedOpeningDelimiter(line); nested {
			stack = append(stack, nestedDelim)
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom += lineEnd + 1
			continue
		}

		// Check for a closing marker: the delimiter alone on its own
		// line (with optional whitespace). The marker must match the
		// top of the stack to pop; a non-matching marker is body
		// content. See TheoryOfNestedBlockParsing.
		trimmedLine := strings.TrimSpace(line)
		if len(stack) > 0 && trimmedLine == stack[len(stack)-1] {
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				bodyEnd = searchFrom
				if lineEnd == -1 {
					blockEnd = len(content)
				} else {
					blockEnd = searchFrom + lineEnd + 1
				}
				return bodyEnd, blockEnd, true
			}
			if lineEnd == -1 {
				return 0, 0, false
			}
			searchFrom += lineEnd + 1
			continue
		}

		if lineEnd == -1 {
			return 0, 0, false
		}
		searchFrom += lineEnd + 1
	}
}

// extractDelimiter extracts the delimiter string from the opening line.
// The delimiter is the text from the start of the (trimmed) line up to the
// first whitespace or '<' character, whichever comes first. A line with no
// characters before whitespace or '<' yields an empty string, causing the
// marker to be skipped. See TheoryOfBoundaryUniqueness.
func extractDelimiter(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '<' {
			return s[:i]
		}
	}
	return s
}

// BlockParseError is returned by ParseFirstBlock for unclosed heredoc blocks.
// An unclosed block is an opening marker with no matching closing delimiter
// line. During streaming this may indicate incomplete output rather than a
// definitive error. See TheoryOfBoundaryUniqueness.
type BlockParseError struct {
	BlockKind string
	Boundary  string
}

func (e *BlockParseError) Error() string {
	return fmt.Sprintf("unclosed block: kind %q delimiter %q has no matching closing line", e.BlockKind, e.Boundary)
}
