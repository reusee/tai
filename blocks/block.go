package blocks

import (
	"bytes"
	"fmt"
	"strings"
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
line start, so any collision between a block's boundary and an example boundary shown in
the system prompt (or with another block's boundary in the same response) makes the parser
close at the wrong marker, swallowing subsequent content or dropping modifications.
Therefore the boundary must be a freshly generated random pair of uncommon, meaningless
Chinese characters, never copied from the illustrative examples. The example blocks in the
system prompt deliberately use distinct boundaries to demonstrate this rule, and those exact
strings are forbidden for reuse. The randomness of the boundary is the integrity guarantee of
the format.

A block opened with boundary X must be closed with the same boundary X. A closing marker
:::end Y where Y differs from the opening boundary is a mismatched boundary error: the
parser reports it instead of silently dropping the block, because a mismatch indicates the
model lost track of which boundary it used. This is distinct from an unclosed block (no
:::end marker at all), which may simply be incomplete during streaming and is tolerated
until more output arrives. Surfacing mismatched-boundary errors prevents the silent loss of
modifications when the model closes the wrong block.
`

const BlockFormatSystemPrompt = `**Structured Output Format (Boundary-Delimited):**

Your response can include structured content using delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
:::<kind> <boundary>
<kind-specific content>
:::end <boundary>

- <kind>: The type of block. The valid kinds and their content formats are defined by the specific kind documentation.
- <boundary>: A random string composed of two uncommon meaningless Chinese characters. A sufficiently random boundary ensures it cannot conflict with any content. Use a different boundary for each block in the same response. The same boundary MUST be used for the start and end markers of a single block.
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

**Boundary Matching (CRITICAL):**
- The closing marker MUST use the EXACT same boundary string as the opening marker. A block opened with :::change 徕珑 MUST be closed with :::end 徕珑, never :::end 栢彣 or any other boundary.
- A mismatched closing marker is a HARD ERROR. The parser rejects the entire block and reports an error, so all modifications in that block are LOST. Never close a block with a different boundary than you opened it with.
- Before writing each :::end marker, verify its boundary matches the corresponding opening ::: marker of the same block. The most common cause of mismatched boundaries is copying a boundary from another block or from an example instead of reusing the one you opened with.
`

// Block represents a parsed boundary block.
type Block struct {
	Kind     string
	Boundary string
	Body     string
}

func ParseFirstBlock(content []byte) (block Block, start int, end int, ok bool, err error) {
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
		boundary := strings.TrimSpace(parts[1])
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

		// Scan for the next :::end marker at line start and compare its
		// boundary against the opening marker's boundary. A matching
		// boundary closes the block. A line-start :::end with a different
		// boundary is a mismatched boundary error: the model closed the
		// wrong block, so the error is surfaced instead of silently
		// dropping the block. The absence of any :::end marker is an
		// unclosed (possibly incomplete) block, tolerated during streaming.
		// See TheoryOfBoundaryUniqueness.
		endMarkerPrefix := []byte(":::end ")
		searchEndFrom := bodyStart
		validEnd := -1
		mismatchedBoundary := ""
		mismatchedEnd := 0
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
			if lineEndOffset == -1 {
				// The end marker line is incomplete (no newline yet).
				// During streaming this may be a fragment, so treat it
				// as incomplete rather than a definitive mismatch.
				break
			}
			endLine := string(content[lineContentStart : lineContentStart+lineEndOffset])
			endBoundary := strings.TrimSpace(endLine)
			if endBoundary == boundary {
				validEnd = candidate
				break
			}
			// Mismatched boundary: a line-start :::end with a different
			// boundary. Record the first one and continue searching, in
			// case a matching marker appears later (an orphan end marker
			// embedded in the body).
			if mismatchedBoundary == "" {
				mismatchedBoundary = endBoundary
				mismatchedEnd = lineContentStart + lineEndOffset + 1
			}
			searchEndFrom = lineContentStart + lineEndOffset + 1
		}
		if validEnd == -1 {
			if mismatchedBoundary != "" {
				// A line-start :::end with a different boundary was found
				// but no matching :::end boundary exists. This is a
				// definitive error: the model closed the block with the
				// wrong boundary. Advance past the mismatched end marker so
				// the caller can consume the malformed block and continue
				// parsing subsequent content.
				return Block{}, blockStart, mismatchedEnd, false, &BlockParseError{
					Mismatched: true,
					Opened:     boundary,
					Closed:     mismatchedBoundary,
				}
			}
			// Opening marker found but no end marker at all. During
			// streaming this may be incomplete; report as unclosed so the
			// caller can distinguish a transient incomplete block from a
			// definitive mismatch.
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

// BlockParseError is returned by ParseFirstBlock for unclosed or mismatched
// boundary blocks. The Mismatched field distinguishes a definitive mismatched
// boundary (the model closed the wrong block) from an unclosed block (no end
// marker at all), which may be incomplete during streaming. See
// TheoryOfBoundaryUniqueness.
type BlockParseError struct {
	Mismatched bool
	Opened     string
	Closed     string
	BlockKind  string
	Boundary   string
}

func (e *BlockParseError) Error() string {
	if e.Mismatched {
		return fmt.Sprintf("mismatched boundary: block opened with %q but closed with %q", e.Opened, e.Closed)
	}
	return fmt.Sprintf("unclosed block: kind %q boundary %q has no matching end marker", e.BlockKind, e.Boundary)
}