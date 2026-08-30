package blocks

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const TheoryOfBlockFormatGeneral = `
The heredoc block format is a general-purpose structured output format for
AI models; BlockFormatSystemPrompt is itself the theory text for the marker
structure, the delimiter selection policy, and the line-start requirement,
and those rules are not repeated here. Only the delimiter string and the
header structure are unified; the body content is defined by the specific
kind.

Kind generality: the block format prompt describes only the format and the
rules that apply to every kind — marker structure, delimiter policy,
deferred execution. It must not mention or reference any specific kind's
semantics, and must not assume which kinds a deployment provides, requires,
or forbids. Third-party programs may embed the format prompt without any
tai-implemented kind, so every kind-specific statement belongs in that kind's
own prompt.

Delimiter rarity is the integrity rationale behind the policy: content —
source code, prompts, and configuration text — is overwhelmingly ASCII or
common-script writing, so a rare Han pair has negligible probability of
appearing in a block body, satisfying the body-disjointness guarantee
without requiring the model to scan its output for collisions. The fixed
two-character length keeps the delimiter visually distinct and its token
cost constant.

The line-start rule is stated twice in the prompt — abstractly, and as a
mechanical emission-time self-check — because models occasionally violate
the abstract rule but can execute a concrete check; naming the common
violation shapes (prose run-on, list bullets, code fences) tells the model
where the risk concentrates.

Unclosed block detection: an opening marker at line start without a
matching closing line is a malformed block; the parser reports an error
rather than silently skipping it.

Delimiter rule centralization: the delimiter rules live only in
BlockFormatSystemPrompt. Individual kind prompts describe only their
kind-specific semantics. The deferred-execution contract is centralized the
same way; see TheoryOfDeferredExecution.
`

const TheoryOfDeferredExecution = `
Blocks are a request protocol, not a tool-call protocol. The model emits
blocks inside one response; the loop parses and processes them only after
the response ends, and every outcome — shell and go-test output,
ingest-block fetches, go-src sources, change-block apply results — is
fed back as user content at the start of the next round. Nothing executes
and nothing returns mid-response. Change blocks are the same: applied
atomically after the round succeeds, with apply errors reported in the
next round.

The distinction matters because models trained on tool-call APIs expect a
result before continuing generation; under the block protocol that
expectation produces hallucinations: the model writes as if a command had
already run or a file had already been fetched, fabricating outputs that
never existed and sometimes building later blocks on the fabricated
results. BlockFormatSystemPrompt states the deferred-execution contract
explicitly for this reason and owns it: kind prompts may state
kind-specific arrival details (which round the output arrives in,
stop-and-wait rules) but must not restate the general principle,
mirroring the delimiter rule centralization (see
TheoryOfBlockFormatGeneral).
`

const TheoryOfBoundaryUniqueness = `
The delimiter is the sole disambiguator between consecutive blocks within a
single response. The parser closes a block at the first line that exactly
matches the opening marker's delimiter. A line that does not match the
delimiter is treated as body content; if no matching delimiter line is
found, the block is unclosed. The closing marker line does not require a
trailing newline: when the delimiter is the last content in the buffer, the
parser extracts it from the remaining content, and an incomplete (still
streaming) delimiter — a shorter extracted string — will not match, so the
block remains unclosed until the full delimiter arrives. One lenient
closing form is accepted: a line whose trimmed content is the delimiter
followed by ">>", optionally separated by whitespace, closes the block like
the exact form; see TheoryOfLenientClosingMarkers.

BlockFormatSystemPrompt is itself the theory text for the delimiter
selection policy, and TheoryOfBlockFormatGeneral owns the rarity rationale
behind it; neither is repeated here. The parser enforces the Han
requirement at extraction time: extractDelimiter validates that the
delimiter is exactly two Unicode Han characters, rejecting ASCII, other
scripts, and any other length, so only two-character Chinese delimiters are
recognized as block markers.

Body-disjointness carries the same weight as the anti-reuse guarantee:
because the parser closes the block at the first line matching the
delimiter, a body line equal to the delimiter prematurely terminates the
block and discards the remaining content — violating either integrity
guarantee corrupts the block.
`

const TheoryOfNestedBlockParsing = `
The parser supports nested blocks: when a block body contains another block opening
marker (<<DELIMITER kind(...)), the inner block's closing marker does not prematurely
close the outer block. The closing-marker scanner maintains a delimiter stack
initialized with the outer block's delimiter. A line that starts with "<<" and
contains a valid function-call header after the delimiter is treated as a nested opening
only when its delimiter matches the outer block's delimiter; matching delimiters are
pushed onto the stack. A line that is a closing marker of the delimiter at the top of
the stack — the delimiter alone on its own line, or the lenient delimiter+">>" form
(see TheoryOfLenientClosingMarkers) — pops the stack; when the stack becomes empty,
the outer block is closed.

The header validation after the delimiter prevents false positives from content that
starts with "<<" but is not a block opening.
`

const TheoryOfBlockFormat = `
The parser uses a heredoc-style block format. The delimiter precedes the header:
<<DELIMITER kind(param1="value1", ...) ... DELIMITER. The delimiter is extracted as the text
between << and the first whitespace or ( character on the opening line. The closing marker
is the delimiter alone on its own line (with one lenient exception: a trailing ">>"; see
TheoryOfLenientClosingMarkers).

An opening marker whose line extends to the end of the content (no trailing newline)
is a truncated block. The parser reports an unclosed-block error.

An opening line with a valid two-character Han delimiter followed by an invalid or
incomplete function-call header is a malformed block reported as a parse error.
`

// TheoryOfLenientOpeningMarkers documents the lenient acceptance of opening
// markers glued to the end of a prose line. See the constant body for the
// rule and its rationale.
const TheoryOfLenientOpeningMarkers = `
Models occasionally glue the opening marker to the end of a prose line,
immediately after a punctuation mark, violating the line-start rule. The
parser accepts this shape leniently for the common Chinese and English
punctuation marks: the marks that end a sentence or a clause, the marks
that close a quotation or a bracket, the ellipsis, and the em dash, in
their ASCII, fullwidth, and CJK forms. lenientPunctuationMarks holds the
exact set. When << directly follows such a mark and the rest of the line
parses as a complete opening (a valid two-character Han delimiter plus a
valid or bare kind header, with nothing after it on the line), the marker
is treated as an opening candidate.

The lenient form yields a block only when its closing delimiter is found
later. A lenient opening that never closes, or whose header is malformed, is
not a block and not a parse error: the marker stays regular content, scanning
continues past it, and it never surfaces in ParseErrors. This differs from
the strict line-start form, where an unclosed or malformed opening is a parse
error fed back for self-correction.

Nesting detection inside a block body remains strict: only line-start nested
openings are tracked by the closing-marker stack. The prompts keep teaching
the line-start rule unchanged; the leniency is a recovery path for
nonconforming output, not an advertised format.
`

// TheoryOfLenientClosingMarkers documents the lenient acceptance of a
// closing line carrying a trailing ">>" after the delimiter. See the
// constant body for the rule and its rationale.
const TheoryOfLenientClosingMarkers = `
Models occasionally emit a closing line with a trailing ">>" glued to the
delimiter — DELIMITER>> — mirroring the "<<" prefix of the opening marker.
The parser accepts this shape silently: after trimming the line, a closing
marker is the delimiter alone, or the delimiter followed by ">>",
optionally separated by whitespace. The lenient form pops the delimiter
stack like the exact form, so it closes nested blocks too, and the
streaming guarantee is preserved: a partial trailing form — a single ">" —
does not match, so a block streamed up to that byte stays unclosed until
the line completes.

Like the lenient opening markers and the attribute-only header, this is a
recovery path for nonconforming output, not an advertised format: no
prompt teaches it, and the delimiter-alone line remains the only taught
closing form. Acceptance only widens what closes a block; any other
trailing content still leaves the block unclosed and reported with
collision hints.
`

const TheoryOfBareKinds = `
Models may emit block opening markers with a bare kind when no parameters are needed:
<<DELIMITER kind instead of <<DELIMITER kind(). Both forms are accepted on equal footing.
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
<<DELIMITER kind(param1="value1", param2="value2")
<kind-specific content>
DELIMITER

- DELIMITER: An uncommon Chinese two-character word (e.g., 龃龉) that does not appear in the block body. The rarity of the characters ensures the delimiter cannot conflict with any content. Use a different pair of uncommon Chinese characters for each block in the same response. The same delimiter MUST be used for the start marker and the closing line.
- kind: The type of block, specified as a function name. The kind name may contain hyphens (e.g., parse-input, record-entry). The valid kinds and their content formats are defined by the specific kind documentation. Parameters on the function call provide kind-specific metadata as named arguments.
- Parameters: Named arguments inside parentheses, in the form param="value". Values are quoted with single or double quotes. If no parameters are needed, the parentheses may be omitted entirely (just the kind name).
- Content: The body between the start marker and the closing line is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but the opening marker must start at the beginning of its own line and the closing delimiter must be on its own line.
- If no blocks are needed, simply omit them.

**Line-Start Requirement (CRITICAL):**
- The opening marker (<<DELIMITER kind(...)) MUST appear at the beginning of a line — immediately after a newline character or at the very start of the response.
- **Self-check (run it every time)**: Before emitting <<, look at the character you are writing it after. It MUST be a newline — or nothing, when the marker is the first output of the response. When anything else precedes it — a word, a punctuation mark, a space, a list bullet, a code fence — emit a newline first, then the marker.
- The closing marker (DELIMITER) MUST appear on its own line — the delimiter alone, with nothing else on that line.
- NEVER place the opening marker at the end of a line of text. If prose immediately precedes a block, end the prose with a newline first, then start the marker on its own new line.
- Any ` + "`<<`" + ` that is not at the start of a line is treated as regular content and will NOT be recognized as a block marker; the block will be silently ignored and its content will be lost.
- Do this (marker starts on its own line after the prose):
  Some explanation text.
  <<龃龉 example(param="value")
  <block body>
  龃龉

**Delimiter Uniqueness (CRITICAL):**
- Generate a fresh delimiter for each block: an uncommon Chinese two-character word (e.g., 彳亍).
- **Never reuse a delimiter that appears in any example in this prompt.** The example delimiters are illustrative only; copying them causes the parser to mismatch closing markers and corrupt blocks.
- Each block in a response must use a distinct pair of uncommon Chinese characters so the parser can unambiguously pair each opening marker with its closing line.
- **Body-disjointness (HARD REQUIREMENT)**: The delimiter MUST NOT appear anywhere in the block body (the code or text between the markers). Because the parser closes the block at the first line matching the delimiter, a body line that matches the delimiter prematurely closes the block and truncates all remaining content. Two uncommon Chinese characters are very unlikely to appear in code or prose, but MUST verify the chosen pair is absent from the body before emitting the block. This is not a suggestion: a delimiter that appears in the body corrupts the block.

**Delimiter Matching (CRITICAL):**
- The opening marker and the closing line form a MATCHED PAIR: a block opened with <<龃龉 example(...) MUST be closed with the EXACT same delimiter string 龃龉, never 彳亍 or any other delimiter.
- A closing line that does not match the opening delimiter is treated as body content, not a closing marker. The parser continues scanning for the matching delimiter; if no matching closing line is found, the block is unclosed — the opening marker's block never completes and its content is discarded.
- Always close a block with the same delimiter used to open it. Before writing each closing line, verify it matches the opening delimiter of the same block. The most common cause of mismatched delimiters is copying a delimiter from another block or from an example instead of reusing the opening delimiter.

**Deferred Execution — Blocks Are Not Tool Calls (CRITICAL):**
- Blocks do not execute while you are generating. Every block, regardless of kind, is processed only after the current response has completely ended: nothing runs, nothing is fetched, and nothing is modified while you are still writing.
- All actions and information requested by blocks arrive only in the NEXT round, as user content at its start. None of it is available in the current response.
- This is the essential difference from tool calls, where a result returns before you continue generating. There is no mid-response result under the block protocol.
- NEVER assume a block you emitted has already executed or produced a result. NEVER fabricate, quote, or reason from an imagined result. A phrase like "the block above returned ..." inside the same response is a hallucination — the block has not run.
- Any content that depends on a block's result — another block, an analysis, a conclusion — MUST be emitted in a later round, after the result has arrived as user content.
- The flow is always: emit the blocks, end the response, and wait; read the results that arrive as the next user message, then continue.
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

		// The opening marker must be at the beginning of a line. As a
		// lenient exception, a marker glued directly after a common
		// Chinese or English punctuation mark at the end of a prose
		// sentence is accepted as an opening candidate; see
		// TheoryOfLenientOpeningMarkers.
		lenient := false
		if idx > 0 && content[idx-1] != '\n' {
			if !precededByPunctuation(content, idx) {
				searchFrom = idx + 2
				continue
			}
			lenient = true
		}
		blockStart := idx

		// Extract the opening line after <<. When the line extends to
		// the end of the content (no trailing newline), the block is
		// necessarily unclosed: the closing marker must be alone on its
		// own line, which cannot exist after EOF. The line is still
		// parsed as an opening marker so the truncation is reported as
		// an unclosed-block error instead of silently dropping the
		// block as prose. The lenient punctuation-glued form is the
		// exception: its unclosed result is skipped silently. See
		// TheoryOfBlockFormat and TheoryOfLenientOpeningMarkers.
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
			if lenient && !r.ok {
				// A lenient opening that never closes, or whose
				// header is malformed, is regular content, not a
				// parse error. Skip it and keep scanning so later
				// block markers are still found. See
				// TheoryOfLenientOpeningMarkers.
				searchFrom = idx + 2
				continue
			}
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
	trimmedOpeningLine := strings.TrimSpace(openingLine)
	rest := strings.TrimSpace(trimmedOpeningLine[len(delimiter):])

	var kind string
	var attrs map[string]string
	if rest == "" {
		kind = ""
		attrs = nil
	} else {
		var valid bool
		kind, attrs, valid = parseHeader(rest)
		if !valid {
			blockKind := extractKindName(rest)
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
				Reason:    "has an invalid or incomplete function-call header",
			}
			return
		}
	}

	matched = true
	result.block.Kind = kind
	result.block.Boundary = delimiter
	result.block.Attributes = attrs
	bodyStart := lineEnd + 1
	if bodyStart > len(content) {
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

// lenientPunctuationMarks lists the punctuation marks after which an opening
// marker glued to the end of a prose line is accepted leniently: the marks
// that end a sentence or a clause and the marks that close a quotation or a
// bracket, in their ASCII and Chinese/fullwidth forms. Opening brackets and
// quotes are excluded, because prose does not end with them. The marks are
// complete runes, so byte-suffix matching never splits a multi-byte rune.
// See TheoryOfLenientOpeningMarkers.
var lenientPunctuationMarks = []string{
	// ASCII sentence and clause enders, closing brackets, closing quotes.
	".", ",", ";", ":", "!", "?", ")", "]", "}", "'", "\"", ">",
	// Chinese and fullwidth marks, including CJK closing brackets and
	// quotes, the ellipsis, and the em dash.
	"。", "．", "，", "、", "；", "：", "！", "？", "…", "—",
	"）", "］", "｝", "＞", "》", "〉", "】", "」", "』", "”", "’",
}

// precededByPunctuation reports whether the << marker at byte offset idx in
// content is immediately preceded by a common Chinese or English punctuation
// mark (see lenientPunctuationMarks): the lenient opening form glued to the
// end of a prose sentence. See TheoryOfLenientOpeningMarkers.
func precededByPunctuation(content []byte, idx int) bool {
	prefix := content[:idx]
	for _, mark := range lenientPunctuationMarks {
		if bytes.HasSuffix(prefix, []byte(mark)) {
			return true
		}
	}
	return false
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
	trimmedAfterMarker := strings.TrimSpace(afterMarker)
	rest := strings.TrimSpace(trimmedAfterMarker[len(delimiter):])
	if rest == "" {
		return delimiter, true
	}
	token, consumed, valid := TokenizeHeader(rest)
	if !valid || token.Kind == "" {
		return "", false
	}
	remaining := strings.TrimSpace(rest[consumed:])
	if remaining != "" {
		return "", false
	}
	return delimiter, true
}

// isClosingMarkerLine reports whether a trimmed line closes a block of the
// given delimiter: the delimiter alone, or — as a lenient exception — the
// delimiter followed by ">>", optionally separated by whitespace. The
// trailing form mirrors the "<<" prefix of the opening marker: a model
// appending ">>" to the delimiter intends the block to close there. A
// partial trailing form (a single ">") does not match, so streaming keeps
// the block unclosed until the line completes. See
// TheoryOfLenientClosingMarkers.
func isClosingMarkerLine(trimmed []byte, delimiter string) bool {
	if string(trimmed) == delimiter {
		return true
	}
	rest, ok := bytes.CutPrefix(trimmed, []byte(delimiter))
	if !ok {
		return false
	}
	return string(bytes.TrimSpace(rest)) == ">>"
}

// findClosingMarker searches for the closing marker of the delimiter within
// the content starting from bodyStart: a line whose trimmed content is the
// delimiter alone, or — as a lenient exception — the delimiter followed by
// ">>". See TheoryOfLenientClosingMarkers. It uses a stack-based approach
// to handle nested blocks: lines starting with "<<" that contain a valid
// XML opening tag push their delimiter onto the stack, and a closing marker
// pops the stack only if it matches the top. The outer block closes when
// the stack becomes empty. See TheoryOfNestedBlockParsing.
func findClosingMarker(content []byte, bodyStart int, delimiter string) (bodyEnd, blockEnd int, found bool) {
	stack := []string{delimiter}
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

		// Check for closing marker: the delimiter alone on its own
		// line (with optional whitespace), or the lenient
		// delimiter+">>" form. See TheoryOfNestedBlockParsing and
		// TheoryOfLenientClosingMarkers.
		trimmed := bytes.TrimSpace(lineBytes)
		if len(stack) > 0 && isClosingMarkerLine(trimmed, stack[len(stack)-1]) {
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

// extractDelimiter extracts the delimiter from an opening marker line: the
// text from the start of the trimmed line up to the first whitespace or
// '(', whichever comes first. The delimiter must be exactly two Unicode
// Han characters; any other length or non-Han character returns an empty
// string.
func extractDelimiter(s string) string {
	s = strings.TrimSpace(s)
	delimiter := s
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '(' {
			delimiter = s[:i]
			break
		}
	}
	runes := []rune(delimiter)
	if len(runes) != 2 {
		return ""
	}
	for _, r := range runes {
		if !unicode.Is(unicode.Han, r) {
			return ""
		}
	}
	return delimiter
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
