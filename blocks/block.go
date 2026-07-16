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
Each block has a kind and a body.
Only the start marker (:::kind boundary) and end marker (:::end boundary) are unified structure.
The content between the markers is defined by the specific kind.
This format replaces ad-hoc XML or JSON escaping with a simple, parseable structure.

**Line-start requirement**: The opening marker (:::kind boundary) and the closing marker
(:::end boundary) must each appear at the beginning of a line. Any occurrence of ":::"
that is not at the start of a line is treated as regular content and will not start a block.
Models tend to glue the opening marker to the end of a preceding prose line rather than
starting it on its own line, which causes the block to be silently ignored. The system
prompt must therefore emphasize this rule with explicit correct/incorrect examples so the
model internalizes the newline-before-marker discipline.

**No surrounding blank lines**: Blocks do not require blank lines before or after them.
A block can appear directly adjacent to other text or other blocks; the only structural
requirement is that each marker starts at the beginning of a line.

**Unclosed block detection**: An opening marker at line start without a matching closing
marker is a malformed block. The parser reports an error rather than silently skipping it,
ensuring that incomplete output from the AI is surfaced to the user.
`

const TheoryOfBoundaryUniqueness = `
The boundary string is the sole disambiguator between consecutive delimited blocks within
a single response. The parser closes a block at the first :::end <boundary> marker found at
line start whose boundary matches the opening marker's boundary. A line-start :::end with a
different boundary is treated as body content, not a closing marker, because the body may
legitimately contain example end markers or other text that starts with ":::end ". The
parser does not report a mismatched-boundary error; it simply continues scanning for the
matching boundary. If no matching :::end <boundary> is found, the block is unclosed.
The closing marker line does not require a trailing newline: when the end marker is the
last content in the buffer, the boundary is extracted from the remaining content. If the
boundary is incomplete (still streaming), the shorter extracted string will not match, so
the block remains unclosed until the full boundary arrives.

Therefore the boundary must be a freshly generated random pair of uncommon, meaningless
Chinese characters, never copied from the illustrative examples. The example blocks in the
system prompt deliberately use distinct boundaries to demonstrate this rule, and those exact
strings are forbidden for reuse. The randomness of the boundary is the integrity guarantee of
the format: if the model reuses an example boundary, a subsequent real block opened with that
same boundary would close at the wrong marker.

The boundary characters must also be disjoint from the block body. Because the parser closes
the block at the first line-start :::end whose boundary matches, a body line that begins with
":::end " followed by the same two ideographs would prematurely terminate the block and
discard all remaining content. Block bodies are predominantly source code (ASCII), so most
Han characters never occur in them; the collision risk arises when the body contains Chinese
comments, string literals, or documentation that reproduces block markers. The model should
therefore select rare, uncommon ideographs that do not appear anywhere in the code or text it
is about to emit, satisfying the anti-reuse guarantee and the body-disjointness guarantee
simultaneously.
`

const BlockFormatSystemPrompt = `**Structured Output Format (Boundary-Delimited):**

Your response can include structured content using delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
:::<kind> <boundary>
<kind-specific content>
:::end <boundary>

- <kind>: The type of block. The valid kinds and their content formats are defined by the specific kind documentation.
- <boundary>: A random string composed of two uncommon meaningless Chinese characters that do not appear in the block body. A sufficiently random boundary ensures it cannot conflict with any content. Use a different boundary for each block in the same response. The same boundary MUST be used for the start and end markers of a single block.
- Content: The body between the start and end markers is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- No blank lines are required before or after a block. A block can appear on consecutive lines with other text or other blocks, but every marker must start at the beginning of its own line.
- If no blocks are needed, simply omit them.

**Line-Start Requirement (CRITICAL):**
- The opening marker (:::<kind> <boundary>) and the closing marker (:::end <boundary>) MUST each appear at the beginning of a line — immediately after a newline character or at the very start of the response.
- NEVER place a marker at the end of a line of text. If you have prose immediately before a block, end the prose with a newline first, then start the marker on its own new line.
- Any ":::" that is not at the start of a line is treated as regular content and will NOT be recognized as a block marker; the block will be silently ignored and your changes will be lost.
- Do this (marker starts on its own line after the prose):
  Some explanation text.
  :::change 徕珑
  <code here>
  :::end 徕珑
- NOT this (marker glued to the end of the prose line — the block will NOT be parsed and your changes will be lost):
  Some explanation text.:::change 徕珑
  <code here>
  :::end 徕珑

**Boundary Uniqueness (CRITICAL):**
- Generate a fresh random pair of two uncommon, meaningless Chinese characters as the boundary for each block.
- **Never reuse a boundary string that appears in any example in this prompt.** The example boundaries are illustrative only; copying them causes the parser to mismatch :::end markers and corrupt blocks.
- Each block in a response must use a distinct boundary so the parser can unambiguously pair each opening marker with its closing marker.
- **Avoid body-content characters**: Select boundary characters that do not appear anywhere in the block body (the code or text between the markers). A body line that starts with ":::end " followed by the same boundary prematurely closes the block and truncates the remaining content. Since block bodies are predominantly source code (ASCII), most Han characters are safe; pick rare, uncommon ideographs absent from any Chinese comments, string literals, or documentation you are about to emit.

**Boundary Matching (CRITICAL):**
- The closing marker MUST use the EXACT same boundary string as the opening marker. A block opened with :::change 徕珑 MUST be closed with :::end 徕珑, never :::end 栢彣 or any other boundary.
- A line-start :::end with a different boundary is treated as body content, not a closing marker. The parser continues scanning for the matching boundary. If no matching :::end is found, the block remains unclosed. Always close a block with the same boundary you opened it with.
- Before writing each :::end marker, verify its boundary matches the corresponding opening ::: marker of the same block. The most common cause of mismatched boundaries is copying a boundary from another block or from an example instead of reusing the one you opened with.
`

// Block represents a parsed boundary block.
type Block struct {
	Kind     string
	Boundary string
	Body     string
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
			searchFrom = idx + 3 // skip past this ":::"
			continue
		}
		blockStart := idx

		// Extract kind and boundary from the opening line
		lineStart := idx + 3
		lineEnd := bytes.IndexByte(content[lineStart:], '\n')
		if lineEnd == -1 {
			searchFrom = idx + 1
			continue
		}
		lineEnd += lineStart
		openingLine := string(content[lineStart:lineEnd])
		parts := strings.SplitN(strings.TrimSpace(openingLine), " ", 2)
		if len(parts) != 2 {
			searchFrom = idx + 1
			continue
		}
		kind := strings.TrimSpace(parts[0])
		boundary := extractHanBoundary(parts[1])
		if kind == "" || boundary == "" {
			searchFrom = idx + 1
			continue
		}

		// :::end is a closing marker, never an opening marker.
		if kind == "end" {
			searchFrom = lineEnd + 1
			continue
		}

		block.Kind = kind
		block.Boundary = boundary

		// Body is everything between the opening line and the end marker.
		bodyStart := lineEnd + 1

		// Scan for the matching :::end <boundary> marker at line start.
		// A line-start :::end with a different boundary is treated as body
		// content, not a closing marker, because the body may legitimately
		// contain example end markers or other text starting with ":::end ".
		// Only a :::end whose boundary matches the opening marker's boundary
		// closes the block. If no matching end marker is found, the block is
		// unclosed (possibly incomplete during streaming).
		// See TheoryOfBoundaryUniqueness.
		endMarkerPrefix := []byte(":::end ")
		searchEndFrom := bodyStart
		validEnd := -1
		for {
			offset := bytes.Index(content[searchEndFrom:], endMarkerPrefix)
			if offset == -1 {
				break
			}
			candidate := searchEndFrom + offset
			// The closing marker must be at the beginning of a line.
			if candidate > 0 && content[candidate-1] != '\n' {
				searchEndFrom = candidate + len(endMarkerPrefix)
				continue
			}
			// Extract the boundary from this end marker line.
			lineContentStart := candidate + len(endMarkerPrefix)
			lineEndOffset := bytes.IndexByte(content[lineContentStart:], '\n')
			var endLine string
			if lineEndOffset == -1 {
				// The end marker is the last line without a trailing newline.
				// Extract the boundary from the remaining content. If the
				// boundary is incomplete (still streaming), extractHanBoundary
				// returns a shorter string that won't match, so this is safe.
				// See TheoryOfBoundaryUniqueness.
				endLine = string(content[lineContentStart:])
			} else {
				endLine = string(content[lineContentStart : lineContentStart+lineEndOffset])
			}
			endBoundary := extractHanBoundary(endLine)
			if endBoundary == boundary {
				validEnd = candidate
				break
			}
			// A line-start :::end with a different boundary is body
			// content. Continue scanning for the matching boundary.
			if lineEndOffset == -1 {
				// No more content to scan; stop searching.
				break
			}
			searchEndFrom = lineContentStart + lineEndOffset + 1
		}
		if validEnd == -1 {
			if final {
				// At Flush, treat an unclosed block as ended: the body is
				// all remaining buffered content after the opening line, and
				// the entire buffer is consumed so post-flush content does not
				// combine with pre-flush content. See TheoryOfParserState.
				block.Body = strings.TrimSpace(string(content[bodyStart:]))
				start = blockStart
				end = len(content)
				ok = true
				return
			}
			// No matching end marker found. During streaming this may
			// be incomplete; report as unclosed.
			return Block{}, 0, 0, false, &BlockParseError{
				BlockKind: kind,
				Boundary:  boundary,
			}
		}
		bodyEnd := validEnd

		// Extract body text
		block.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
		// Calculate end of entire block
		endLineEnd := bytes.IndexByte(content[bodyEnd:], '\n')
		if endLineEnd == -1 {
			end = len(content)
		} else {
			end = bodyEnd + endLineEnd + 1
		}

		start = blockStart
		ok = true
		return
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
