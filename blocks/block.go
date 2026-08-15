package blocks

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const TheoryOfBlockFormatGeneral = `
The heredoc block format is a general-purpose structured output format for AI
models. Each block has a kind (XML element name), attributes (XML attributes on the
opening tag), and a body. The opening marker is <<DELIMITER <kind attr="..">; the
closing marker is the DELIMITER alone on its own line, with no XML closing tag. Only
the delimiter string and the XML element structure are unified; the body content is
defined by the specific kind. The format leverages familiar heredoc syntax for
parsing reliability.

Delimiter selection policy: every delimiter MUST be exactly three uncommon Chinese
characters (e.g., 徕珑龘). Content — source code, prompts, and configuration text —
is overwhelmingly ASCII or common-script writing, so a rare Han trio has negligible
probability of appearing in a block body, satisfying the body-disjointness guarantee
without requiring the model to scan its output for collisions. The fixed
three-character length keeps the delimiter visually distinct and its token cost
constant. The model must never emit the literal placeholder "DELIMITER", must never
reuse an example delimiter, and must never use a delimiter of any other length or
script.

Line-start requirement: the opening marker must appear at the beginning of a line;
the closing marker (the delimiter alone) must be on its own line. A ` + "`<<`" + ` not at the
start of a line is regular content and will not start a block — models tend to glue
the marker to the end of a preceding prose line, so the system prompt emphasizes the
rule with explicit correct/incorrect examples. No blank lines are required around
blocks: a block may sit directly adjacent to other text or blocks; the only
structural requirements are the line-start opening and the own-line closing marker.

Unclosed block detection: an opening marker at line start without a matching closing
line is a malformed block; the parser reports an error rather than silently skipping
it, so incomplete AI output is surfaced to the user.

Delimiter rule centralization: the delimiter rules (selection, uniqueness,
body-disjointness, matching) live only in BlockFormatSystemPrompt and
BlockFormatRestatePrompt. Individual kind prompts (continue, shell, go-test, summary,
memory, request-context, change, done) must not restate any format rule, template,
or example delimiter; they reference the general format and describe only their
kind-specific semantics — which kind to emit and what content the body carries.
Every component set that processes blocks MUST include BlockFormatSystemPrompt as a
prompt-only component; a kind prompt that assumes the block format is present
without the component set carrying it is a bug. This keeps redundant delimiter
description at zero and gives the format rules a single authoritative source.
`

const TheoryOfBoundaryUniqueness = `
The delimiter is the sole disambiguator between consecutive blocks within a single
response. The parser closes a block at the first line that exactly matches the
opening marker's delimiter. A line that does not match the delimiter is treated as
body content; if no matching delimiter line is found, the block is unclosed. The
closing marker line does not require a trailing newline: when the delimiter is the
last content in the buffer, the parser extracts it from the remaining content, and
an incomplete (still streaming) delimiter — a shorter extracted string — will not
match, so the block remains unclosed until the full delimiter arrives.

The delimiter must be freshly chosen as exactly three uncommon Chinese characters,
never copied from the illustrative examples. The example blocks in the system prompt
deliberately use distinct delimiters to demonstrate this rule, and those exact
strings are forbidden for reuse. Rarity is the integrity guarantee: a trio of
uncommon Chinese characters is effectively absent from code and prose, so the chance
of a body line accidentally matching the delimiter is negligible, while reusing an
example delimiter would cause a subsequent real block opened with that same
delimiter to close at the wrong marker. The parser enforces the Han requirement at
extraction time: extractDelimiter validates that the delimiter is exactly three
Unicode Han characters, rejecting ASCII, other scripts, and any other length, so
only three-character Chinese delimiters are recognized as block markers.

The delimiter must also be disjoint from the block body; this is a hard requirement,
not an optional suggestion. Because the parser closes the block at the first line
matching the delimiter, a body line that matches the delimiter would prematurely
terminate the block and discard all remaining content. The model must therefore
select a delimiter that does not appear anywhere in the code or text it is about to
emit. Three uncommon Chinese characters satisfy this by construction for code and
prose, but the model must verify the chosen trio is absent from the body before
emitting the block. Body-disjointness is as important as the anti-reuse guarantee:
both are integrity guarantees of the format, and violating either corrupts the
block.
`

const TheoryOfNestedBlockParsing = `
The parser supports nested blocks: when a block body contains another block opening
marker (<<DELIMITER <kind ...>), the inner block's closing marker does not prematurely
close the outer block. The closing-marker scanner maintains a delimiter stack
initialized with the outer block's delimiter. A line that starts with "<<" and
contains a valid XML opening tag after the delimiter is treated as a nested opening
only when its delimiter matches the outer block's delimiter; matching delimiters are
pushed onto the stack, marking the start of a nested block. A line that is a
delimiter alone on its own line pops the stack only if it matches the top; when the
stack becomes empty, the outer block is closed. A closing marker that does not match
the top of the stack is treated as body content.

This correctly handles same-delimiter nesting (the inner block's closing marker pops
the inner level, not the outer level). Different-delimiter opening markers in the
body are treated as body content, not pushed onto the stack: they pose no collision
risk, because a different-delimiter closing line will never match the outer block's
delimiter. When a same-delimiter nested block is unclosed, the outer block is also
unclosed, because the stack never returns to empty.

The same-delimiter restriction prevents false positives from body content that
incidentally matches the <<HanHanHan <tag> pattern: if a different-delimiter opening
were pushed, the outer block's closing marker would not match the new stack top and
the block would be incorrectly reported as unclosed. By only tracking same-delimiter
openings, stack pushes are reserved for genuine nesting scenarios where the inner
closing marker must be distinguished from the outer closing marker.

The XML-tag validation after the delimiter prevents false positives from content that
starts with "<<" but is not a block opening: the validator extracts the delimiter,
finds the first "<" in the remainder, and calls TokenizeXMLTag to verify a
well-formed XML tag. Lines like "<<some code" (no XML tag) or "<<text with < angle
brackets>" (invalid XML tag) are treated as body content, not nested openings,
avoiding false nesting from shell heredocs, code comments, or prose that happens to
start with "<<". The validator also checks that the XML tag consumes the entire line
(up to whitespace): a line like "<<FOO <tag> some text" has trailing content after
the tag and is treated as body content, not a nested opening. Without this
trailing-content check, the false nested opening would push "FOO" onto the delimiter
stack, causing the outer block's closing marker to be treated as body content and
the block to be incorrectly reported as unclosed.
`

const TheoryOfBlockFormat = `
The parser uses a heredoc-style block format. The delimiter (a random string)
precedes the kind as an XML opening tag:
<<DELIMITER <kind attr=".."> ... DELIMITER. The delimiter is extracted as the text
between ` + "`<<`" + ` and the first whitespace or ` + "`<`" + ` character on the opening line;
trailing content after the delimiter is skipped by searching for the first ` + "`<`" + ` in
the rest of the line. The closing marker is the delimiter alone on its own line — no
XML closing tag is needed. The delimiter is the sole disambiguator between
consecutive blocks within a single response.

An opening marker whose line extends to the end of the content (no trailing newline)
is a truncated block: the closing marker must be alone on its own line, which cannot
exist after EOF. The parser parses the line as an opening marker and reports an
unclosed-block error, surfacing the truncation instead of silently treating the
marker as prose.

An opening line with a valid three-character Han delimiter followed by an invalid or
incomplete XML opening tag (e.g., a missing ">") is a malformed block, not prose: the
delimiter marks the line as an intended block opening. The parser reports it as a
parse error so the model can correct it, rather than silently dropping the intended
block.
`

const TheoryOfBareKinds = `
Models sometimes emit block opening markers with a bare kind instead of an
XML opening tag: <<DELIMITER kind instead of <<DELIMITER <kind ...>.
Both forms are accepted on equal footing: a bare kind carries no
attributes, and the XML opening tag takes precedence whenever the marker
carries one. The compatibility does not introduce new block boundaries:
a marker line with a compatible token already opened a block, kindless,
before the extension; the extension only gives those blocks the intended
kind. Nested-block detection accepts the same bare form, requiring the
whole marker line to be the bare token so trailing prose is never
mistaken for a nested opening.
`

const TheoryOfKindlessBlocks = `
Models sometimes emit blocks whose opening marker omits the XML opening tag:
<<DELIMITER ... DELIMITER with no kind or attributes. The parser accepts these blocks
with an empty Kind (see tryParseBlock). Because an empty Kind matches no component
and cannot be used for lookup, such blocks can only be located by iterating all
blocks in order — ParseBlocks provides this capability. Consumers that need a
specific kind (e.g., the thought summarizer looking for a summary block) should
first search the parsed blocks by kind and fall back to the first block only when no
kinded block is found, since a kindless block is often the intended output.
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

Use heredoc-delimited blocks to include structured content in responses.
This format avoids escaping issues and is easy to parse.

**Block Format:**
<<DELIMITER <kind attr1=".." attr2="...">
<kind-specific content>
DELIMITER

- DELIMITER: Exactly three uncommon Chinese characters (e.g., 徕珑龘) that do not appear in the block body. The rarity of the characters ensures the delimiter cannot conflict with any content. Use a different trio of uncommon Chinese characters for each block in the same response. The same delimiter MUST be used for the start marker and the closing line.
- <kind>: The type of block, specified as an XML element name. The valid kinds and their content formats are defined by the specific kind documentation. Attributes on the opening tag provide kind-specific metadata.
- Content: The body between the start marker and the closing line is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but the opening marker must start at the beginning of its own line and the closing delimiter must be on its own line.
- If no blocks are needed, simply omit them.

**Line-Start Requirement (CRITICAL):**
- The opening marker (<<DELIMITER <kind ...>) MUST appear at the beginning of a line — immediately after a newline character or at the very start of the response.
- The closing marker (DELIMITER) MUST appear on its own line — the delimiter alone, with nothing else on that line.
- NEVER place the opening marker at the end of a line of text. If prose immediately precedes a block, end the prose with a newline first, then start the marker on its own new line.
- Any ` + "`<<`" + ` that is not at the start of a line is treated as regular content and will NOT be recognized as a block marker; the block will be silently ignored and the changes will be lost.
- Do this (marker starts on its own line after the prose):
  Some explanation text.
  <<徕珑龘 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  徕珑龘
- NOT this (marker glued to the end of the prose line — the block will NOT be parsed and the changes will be lost):
  Some explanation text.<<徕珑龘 <change op="MODIFY" target="Foo" file-path="/home/user/foo.go">
  <code here>
  徕珑龘

**Delimiter Uniqueness (CRITICAL):**
- Generate a fresh delimiter for each block: exactly three uncommon Chinese characters (e.g., 龘靐齉).
- **Never reuse a delimiter that appears in any example in this prompt.** The example delimiters are illustrative only; copying them causes the parser to mismatch closing markers and corrupt blocks.
- Each block in a response must use a distinct trio of uncommon Chinese characters so the parser can unambiguously pair each opening marker with its closing line.
- **Body-disjointness (HARD REQUIREMENT)**: The delimiter MUST NOT appear anywhere in the block body (the code or text between the markers). Because the parser closes the block at the first line matching the delimiter, a body line that matches the delimiter prematurely closes the block and truncates all remaining content. Three uncommon Chinese characters are very unlikely to appear in code or prose, but MUST verify the chosen trio is absent from the body before emitting the block. This is not a suggestion: a delimiter that appears in the body corrupts the block.

**Delimiter Matching (CRITICAL):**
- The opening marker and the closing line form a MATCHED PAIR: a block opened with <<徕珑龘 <change ...> MUST be closed with the EXACT same delimiter string 徕珑龘, never 龘靐齉 or any other delimiter.
- A closing line that does not match the opening delimiter is treated as body content, not a closing marker. The parser continues scanning for the matching delimiter; if no matching closing line is found, the block is unclosed — the opening marker's block never completes and its content is discarded.
- Always close a block with the same delimiter used to open it. Before writing each closing line, verify it matches the opening delimiter of the same block. The most common cause of mismatched delimiters is copying a delimiter from another block or from an example instead of reusing the opening delimiter.
`

const BlockFormatRestatePrompt = `- **Block format (CRITICAL)**: Every block opening marker line MUST start at the beginning of its own line, immediately after a newline. The closing line is the delimiter alone on its own line. NEVER glue the opening marker to the end of a prose line — the block will be silently ignored and the changes will be lost.
- **Header/Footer checklist**: Each block needs TWO markers that form a MATCHED PAIR — never omit or swap either. Opening marker: '<<' followed by a freshly chosen delimiter (exactly three uncommon Chinese characters) and the opening tag '<kind ...>' ending with '>'. Closing marker: the EXACT SAME delimiter alone on its own line.
- **The DELIMITER MUST be exactly three uncommon Chinese characters** (e.g., 徕珑龘, 龘靐齉, 齉爩龖), NEVER the literal text "<DELIMITER>" or a common word. Writing "<<DELIMITER" literally causes the parser to fail to recognize the block and the changes will be silently lost.
- Generate a fresh trio of uncommon Chinese characters for each block. Never reuse a delimiter from any example in this prompt.
- **Delimiter matching (CRITICAL)**: The closing line MUST use the EXACT same delimiter string as the opening marker. A mismatched closing line is treated as body content, not a closing marker: the block stays unclosed and its content is discarded. Before writing each closing line, verify it matches the opening delimiter of the same block.
- **Body-disjointness (HARD REQUIREMENT)**: The delimiter MUST NOT appear anywhere in the block body. This is a hard requirement: a body line matching the delimiter prematurely closes the block and truncates all remaining content. Three uncommon Chinese characters satisfy this by construction for code and prose, but MUST verify the chosen trio is absent from the body before emitting the block.
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

// ParseBlocks parses all complete blocks from content in order. Blocks whose
// opening marker omits the XML opening tag are parsed with an empty Kind;
// such blocks can only be located by iterating all blocks, not by filtering
// by kind. Unclosed blocks are skipped so that complete blocks following
// them are still found. See TheoryOfKindlessBlocks.
func ParseBlocks(content []byte) ([]Block, error) {
	var blocks []Block
	remaining := content
	for len(remaining) > 0 {
		block, _, end, ok, err := ParseFirstBlock(remaining)
		if err != nil {
			// Unclosed block: skip past its opening marker and continue
			// scanning for subsequent complete blocks.
			if end > 0 && end <= len(remaining) {
				remaining = remaining[end:]
				continue
			}
			return blocks, err
		}
		if !ok {
			break
		}
		blocks = append(blocks, block)
		remaining = remaining[end:]
	}
	return blocks, nil
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

		// Extract the opening line after <<. When the line extends to
		// the end of the content (no trailing newline), the block is
		// necessarily unclosed: the closing marker must be alone on its
		// own line, which cannot exist after EOF. The line is still
		// parsed as an opening marker so the truncation is reported as
		// an unclosed-block error instead of silently dropping the
		// block as prose. See TheoryOfBlockFormat.
		lineStart := idx + 2
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		if lineEnd == -1 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}
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
	// extractDelimiter trims leading whitespace internally, so we must
	// also trim before slicing to align the rest-of-line offset with
	// the extracted delimiter. Without this, leading whitespace between
	// << and the delimiter causes rest to start at a wrong byte offset
	// (inside a multi-byte rune), producing garbled text.
	trimmedOpeningLine := strings.TrimSpace(openingLine)
	rest := trimmedOpeningLine[len(delimiter):]

	// The XML opening tag is optional: a model may emit
	// <<DELIMITER ... DELIMITER with no kind or attributes, parsed with
	// an empty Kind; or a bare kind — <<DELIMITER kind — whose first
	// XML-name token is the Kind. See TheoryOfKindlessBlocks and
	// TheoryOfBareKinds.
	var kind string
	var attrs map[string]string
	if ltIdx := strings.Index(rest, "<"); ltIdx != -1 {
		xmlPart := strings.TrimSpace(rest[ltIdx:])
		// In heredoc format, the closing marker is the delimiter alone,
		// so there is no XML closing tag on the opening line to reject.
		// However, reject if the XML part starts with </ as a safety check.
		if strings.HasPrefix(xmlPart, "</") {
			return
		}
		var valid bool
		kind, attrs, valid = parseXMLOpeningTag(xmlPart)
		if !valid || kind == "" {
			// A valid three-character Han delimiter followed by an
			// XML-like tag marks this line as an intended block opening
			// whose opening tag is malformed or incomplete (e.g., a
			// missing '>'). Report it as a parse error instead of
			// silently treating it as prose, so the model can correct
			// it. During streaming the line may be incomplete and the
			// error is transient; it is collected only at Flush.
			// See TheoryOfParseErrorCollection.
			blockKind := extractTagName(xmlPart)
			matched = true
			result.block.Kind = blockKind
			result.block.Boundary = delimiter
			result.block.Attributes = attrs
			result.start = blockStart
			result.end = lineEnd + 1
			if result.end > len(content) {
				result.end = len(content)
			}
			result.err = &BlockParseError{
				BlockKind: blockKind,
				Boundary:  delimiter,
				Content:   string(content[blockStart:]),
				Line:      bytes.Count(content[:blockStart], []byte("\n")) + 1,
				Reason:    "has an invalid or incomplete XML opening tag",
			}
			return
		}
	} else {
		// A bare kind without the XML opening tag: the model may emit
		// <<DELIMITER kind instead of <<DELIMITER <kind ...>. The first
		// XML-name token of the rest is the Kind, and the block has no
		// attributes. An empty or non-token rest stays kindless.
		// See TheoryOfBareKinds.
		kind = extractBareKind(rest)
	}
	matched = true
	result.block.Kind = kind
	result.block.Boundary = delimiter
	result.block.Attributes = attrs
	bodyStart := lineEnd + 1
	if bodyStart > len(content) {
		// The opening line extends to the end of the content, so the
		// body is empty and the block is necessarily unclosed.
		// See TheoryOfBlockFormat.
		bodyStart = len(content)
	}
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
	// of whether Flush has been called. The Content field captures the
	// full text from the opening marker to the end of the available
	// content, providing debugging context for truncated output.
	// See TheoryOfBlockFormat.
	result.start = blockStart
	result.end = lineEnd + 1
	if result.end > len(content) {
		result.end = len(content)
	}
	result.err = &BlockParseError{
		BlockKind: kind,
		Boundary:  delimiter,
		Content:   string(content[blockStart:]),
		Line:      bytes.Count(content[:blockStart], []byte("\n")) + 1,
		Hints:     findDelimiterCollisionHints(content, bodyStart, delimiter),
	}
	return
}

func nestedOpeningDelimiter(line string) (delimiter string, ok bool) {
	if !strings.HasPrefix(line, "<<") {
		return "", false
	}
	afterMarker := line[2:]
	delimiter = extractDelimiter(afterMarker)
	if delimiter == "" {
		return "", false
	}
	// extractDelimiter trims leading whitespace internally, so we must
	// also trim before slicing to align the rest-of-line offset with
	// the extracted delimiter. Without this, leading whitespace between
	// << and the delimiter causes rest to start at a wrong byte offset.
	trimmedAfterMarker := strings.TrimSpace(afterMarker)
	rest := trimmedAfterMarker[len(delimiter):]
	ltIdx := strings.Index(rest, "<")
	if ltIdx == -1 {
		// A bare kind without the XML opening tag is accepted as a
		// nested opening marker, mirroring the bare-kind acceptance in
		// tryParseBlock. Unlike the rendering path, the whole rest must
		// be exactly the bare token: a marker line with trailing prose
		// is not a nested opening, honoring the trailing-content check
		// below. See TheoryOfBareKinds.
		if token := extractBareKind(rest); token == "" || strings.TrimSpace(rest) != token {
			return "", false
		}
		return delimiter, true
	}
	xmlPart := strings.TrimSpace(rest[ltIdx:])
	if strings.HasPrefix(xmlPart, "</") {
		return "", false
	}
	token, consumed, valid := TokenizeXMLTag(xmlPart)
	if !valid || token.IsClosing() || token.Kind == "" {
		return "", false
	}
	// A real block opening marker line consists of only the delimiter and
	// the XML opening tag — no trailing prose. Without this check, body
	// content like "<<FOO <tag> some text" would be falsely detected as a
	// nested block opening, pushing "FOO" onto the delimiter stack. If no
	// line matching "FOO" alone follows, the outer block's closing marker
	// is consumed as body content and the block is incorrectly reported
	// as unclosed. See TheoryOfNestedBlockParsing.
	remaining := strings.TrimSpace(xmlPart[consumed:])
	if remaining != "" {
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
	delimLen := len(delimiter)
	searchFrom := bodyStart
	for {
		lineEnd := bytes.IndexByte(content[searchFrom:], '\n')
		atEOF := lineEnd == -1
		var lineBytes []byte
		if atEOF {
			lineBytes = content[searchFrom:]
		} else {
			lineBytes = content[searchFrom : searchFrom+lineEnd]
		}

		// Only lines starting with "<<" can be nested block openings.
		// This byte-level check avoids string conversion for the vast
		// majority of body lines (code, prose, etc.) that do not start
		// with "<<". See TheoryOfNestedBlockParsing.
		if len(lineBytes) >= 2 && lineBytes[0] == '<' && lineBytes[1] == '<' {
			line := string(lineBytes)
			if nestedDelim, nested := nestedOpeningDelimiter(line); nested && nestedDelim == stack[0] {
				stack = append(stack, nestedDelim)
				if atEOF {
					return 0, 0, false
				}
				searchFrom += lineEnd + 1
				continue
			}
		}

		// Check for closing marker: the delimiter alone on its own line
		// (with optional whitespace). Use byte-level trimming and a
		// length pre-check to avoid string allocation for lines that
		// cannot match the delimiter. See TheoryOfNestedBlockParsing.
		trimmed := bytes.TrimSpace(lineBytes)
		if len(stack) > 0 && len(trimmed) == delimLen && string(trimmed) == stack[len(stack)-1] {
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				bodyEnd = searchFrom
				if atEOF {
					blockEnd = len(content)
				} else {
					blockEnd = searchFrom + lineEnd + 1
				}
				return bodyEnd, blockEnd, true
			}
			if atEOF {
				return 0, 0, false
			}
			searchFrom += lineEnd + 1
			continue
		}

		if atEOF {
			return 0, 0, false
		}
		searchFrom += lineEnd + 1
	}
}

// findDelimiterCollisionHints scans the block body for lines where the
// delimiter appears with leading or trailing text. Such lines are the most
// likely cause of an unclosed block: the model intended them as the closing
// marker but wrote extra text around the delimiter, so the parser does not
// recognize them (the closing line must be the delimiter alone). The hints
// point the model or user at the malformed lines without requiring them to
// scan the entire body in the error output. Line numbers are absolute
// (1-based) positions in the content. See TheoryOfBoundaryUniqueness.
func findDelimiterCollisionHints(content []byte, bodyStart int, delimiter string) (hints []string) {
	lineNo := bytes.Count(content[:bodyStart], []byte("\n")) + 1
	searchFrom := bodyStart
	for searchFrom < len(content) {
		lineEnd := bytes.IndexByte(content[searchFrom:], '\n')
		var line []byte
		if lineEnd == -1 {
			line = content[searchFrom:]
		} else {
			line = content[searchFrom : searchFrom+lineEnd]
		}
		trimmed := bytes.TrimSpace(line)
		trimmedStr := string(trimmed)
		if len(trimmedStr) > len(delimiter) &&
			(strings.HasPrefix(trimmedStr, delimiter) || strings.HasSuffix(trimmedStr, delimiter)) {
			hints = append(hints, fmt.Sprintf("line %d: %q", lineNo, trimmedStr))
		}
		if lineEnd == -1 {
			break
		}
		searchFrom += lineEnd + 1
		lineNo++
	}
	return hints
}

// extractDelimiter extracts the delimiter string from the opening line.
// The delimiter is the text from the start of the (trimmed) line up to the
// first whitespace or '<' character, whichever comes first. The delimiter
// must be exactly three Unicode Han characters; any other length or any
// non-Han character causes an empty string to be returned, so the marker is
// skipped and the line is treated as regular content. This enforces the
// policy that block delimiters are trios of uncommon Chinese characters. A
// line with no characters before whitespace or '<' also yields an empty
// string.
// See TheoryOfBoundaryUniqueness.
func extractDelimiter(s string) string {
	s = strings.TrimSpace(s)
	delimiter := s
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '<' {
			delimiter = s[:i]
			break
		}
	}
	// The delimiter must consist of exactly three Unicode Han characters.
	// Non-Han delimiters and delimiters of any other length are rejected so
	// that only three-character Chinese delimiters are recognized as block
	// markers.
	// See TheoryOfBoundaryUniqueness.
	runes := []rune(delimiter)
	if len(runes) != 3 {
		return ""
	}
	for _, r := range runes {
		if !unicode.Is(unicode.Han, r) {
			return ""
		}
	}
	return delimiter
}

// extractBareKind extracts a bare block kind from the remainder of an
// opening marker line. Models sometimes emit <<DELIMITER kind instead of
// <<DELIMITER <kind ...>; the remainder is trimmed and its first
// whitespace-delimited token is returned when it consists solely of XML
// name characters. An empty remainder, or a first token containing a
// non-name character (a space, a '<', Han text, punctuation), yields "",
// so the block stays kindless. See TheoryOfBareKinds.
func extractBareKind(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if isXMLSpace(c) {
			return rest[:i]
		}
		if !isXMLNameChar(c) {
			return ""
		}
	}
	return rest
}

// extractTagName extracts the element name from a possibly-incomplete XML
// opening tag. Returns "" if no valid XML name is present. Used to report
// the block kind in parse errors for malformed opening tags, where the tag
// could not be fully tokenized by TokenizeXMLTag.
func extractTagName(xmlPart string) string {
	s := strings.TrimSpace(xmlPart)
	if !strings.HasPrefix(s, "<") {
		return ""
	}
	s = s[1:]
	for len(s) > 0 && isXMLSpace(s[0]) {
		s = s[1:]
	}
	start := 0
	for start < len(s) && isXMLNameChar(s[start]) {
		start++
	}
	return s[:start]
}

// maxParseErrorContentLength caps the amount of block content included in a
// BlockParseError message. An unclosed block can have an arbitrarily large
// body (e.g., a model emitting a large file before being cut off); including
// the full body would produce an enormous error message and waste context
// when fed back to the model for self-correction.
const maxParseErrorContentLength = 2000

// truncateParseErrorContent truncates block content for display in error
// messages. Content within the limit is returned unchanged. Larger content
// is reduced to a head and tail portion separated by a truncation note: the
// head preserves the opening marker (which identifies the block), and the
// tail shows where the content ended (where the closing marker was expected).
// Cut points are adjusted to UTF-8 rune boundaries so the truncated output is
// never split mid-rune.
func truncateParseErrorContent(content string) string {
	if len(content) <= maxParseErrorContentLength {
		return content
	}
	headLen := maxParseErrorContentLength * 2 / 3
	tailLen := maxParseErrorContentLength - headLen
	// Adjust the head cut back to a rune boundary.
	for headLen > 0 && !utf8.RuneStart(content[headLen]) {
		headLen--
	}
	// Adjust the tail start forward to a rune boundary.
	tailStart := len(content) - tailLen
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	omitted := tailStart - headLen
	return content[:headLen] +
		fmt.Sprintf("\n...[truncated, %d bytes omitted]...\n", omitted) +
		content[tailStart:]
}

// BlockParseError is returned by ParseFirstBlock for malformed heredoc
// blocks: an unclosed block (an opening marker with no matching closing
// delimiter line) or a block whose opening tag is invalid or incomplete.
// During streaming this may indicate incomplete output rather than a
// definitive error. The Content field holds the full text from the opening
// marker to the end of the available content, providing context for
// debugging. The Line field holds the 1-based line number of the opening
// marker in the content. The Reason field distinguishes malformed opening
// tags from unclosed blocks. The Error message truncates large content to
// keep the feedback bounded; the full content remains available in this
// field. The Hints field lists body lines where the delimiter appears with
// leading or trailing text — the most likely cause of an unclosed block,
// because the closing line must be the delimiter alone.
// See TheoryOfBoundaryUniqueness.
type BlockParseError struct {
	BlockKind string
	Boundary  string
	Content   string
	Line      int
	Reason    string
	Hints     []string
}

func (e *BlockParseError) Error() string {
	prefix := "unclosed block"
	detail := "has no matching closing line"
	if e.Reason != "" {
		prefix = "malformed block"
		detail = e.Reason
	}
	location := ""
	if e.Line > 0 {
		location = fmt.Sprintf(" at line %d", e.Line)
	}
	msg := fmt.Sprintf("%s%s: kind %q delimiter %q %s", prefix, location, e.BlockKind, e.Boundary, detail)
	if len(e.Hints) > 0 {
		msg += "\nhint: these body lines start or end with the delimiter but have extra text, so they are not valid closing markers (the closing line must be the delimiter alone):"
		for _, hint := range e.Hints {
			msg += "\n  " + hint
		}
	}
	msg += fmt.Sprintf("\n\nContent parsed so far:\n%s", truncateParseErrorContent(e.Content))
	return msg
}
