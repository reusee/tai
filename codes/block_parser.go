package codes

import (
	"bytes"
	"strings"
)

const BlockFormatTheory = `
The boundary block format is a general-purpose structured output format for AI models.
It uses delimited blocks with a random boundary string to avoid parsing conflicts with content.
Each block has a kind (e.g., "change") and a body.
Only the start marker (:::kind boundary) and end marker (:::end boundary) are unified structure.
The content between the markers is defined by the specific kind (e.g., XML metadata for change blocks).
This format replaces ad-hoc XML or JSON escaping with a simple, parseable structure.

**Line-start requirement**: The opening marker (:::kind boundary) and the closing marker
(:::end boundary) must each appear at the beginning of a line. Any occurrence of ":::"
that is not at the start of a line is treated as regular content and will not start a block.
`

func BlockFormatSystemPrompt() string {
	return `**Structured Output Format (Boundary-Delimited):**

Your response can include structured content (code changes, etc.) using delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
:::<kind> <boundary>

<kind-specific content>

:::end <boundary>

- <kind>: The type of block, e.g., "change", "memory-item".
- <boundary>: A random string composed of two uncommon meaningless Chinese characters (e.g., 徕珑). Use a different boundary for each block in the same response. The same boundary MUST be used for the start and end markers.
- Content: The body between the start and end markers is defined by the specific kind. See the kind-specific format documentation for details.
- Content outside blocks is preserved verbatim.
- If no blocks are needed, simply omit them.
`
}

// Block represents a parsed boundary block.
type Block struct {
	Kind     string
	Boundary string
	Body     string
}

func ParseFirstBlock(content []byte) (block Block, start int, end int, ok bool) {
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

		// Find a valid :::end BOUNDARY marker at line start
		endMarker := ":::end " + boundary
		searchEndFrom := bodyStart
		validEnd := -1
		for {
			offset := bytes.Index(content[searchEndFrom:], []byte(endMarker))
			if offset == -1 {
				break
			}
			candidate := searchEndFrom + offset
			// The closing marker must be at the beginning of a line.
			if candidate > 0 && content[candidate-1] != '\n' {
				// Not at line start, skip past this occurrence
				searchEndFrom = candidate + len(endMarker)
				continue
			}
			validEnd = candidate
			break
		}
		if validEnd == -1 {
			searchFrom = blockStart + 1
			continue
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