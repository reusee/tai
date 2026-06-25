package codes

import (
	"bytes"
	"encoding/json"
	"strings"
)

// BlockFormatTheory explains the design of the boundary block format.
const BlockFormatTheory = `
The boundary block format is a general-purpose structured output format for AI models.
It uses delimited blocks with a random boundary string to avoid parsing conflicts with content.
Each block has a kind (e.g., "change", "call"), headers as key-value pairs, and a body.
This format replaces ad-hoc XML or JSON escaping with a simple, parseable structure.
`

// BlockFormatSystemPrompt returns a system prompt describing the general boundary block format.
func BlockFormatSystemPrompt() string {
	return `**Structured Output Format (Boundary-Delimited):**

Your response can include structured content (code changes, function calls, etc.) using delimited blocks.
This format avoids escaping issues and is easy to parse.

**Block Format:**
---<kind> <boundary>
[headers]

<body>

---end <boundary>

- <kind>: The type of block, e.g., "change", "call", "memory-item".
- <boundary>: A random string composed of two uncommon meaningless Chinese characters (e.g., 徕珑). Use a different boundary for each block in the same response. The same boundary MUST be used for the start and end markers.
- Headers: Key-value pairs, one per line, separated by ':'. A blank line separates headers from the body.
- Body: The main content of the block. For code changes, provide the complete declaration code. For function calls, provide JSON arguments.
- Content outside blocks is preserved verbatim.
- If no blocks are needed, simply omit them.
`
}

// CallFormatSystemPrompt returns additional rules for call blocks.
func CallFormatSystemPrompt() string {
	return `**Function Call Blocks (kind "call"):**

When you need to invoke a function, use a "call" block with headers "function" (the function name) and optionally "id" (a unique call identifier).
The body must be a JSON object representing the arguments to the function.

Example:

---call 徕珑
function: read_file
id: call_1

{
  "path": "/home/user/foo.go",
  "offset": 0,
  "limit": 100
}

---end 徕珑
`
}

// Block represents a parsed boundary block.
type Block struct {
	Kind     string
	Boundary string
	Headers  map[string]string
	Body     string
}

// ParseBlockConfig configures the block parser.
type ParseBlockConfig struct {
	// KnownHeaders, if set, specifies the only allowed header keys.
	// Any header not in this list will stop header parsing and be treated as body.
	KnownHeaders []string

	// RequiredHeaders specifies header keys that must be present.
	// When all required headers have been collected, header parsing stops
	// even if more key-value lines follow.
	RequiredHeaders []string
}

// ParseFirstBlock parses the first boundary block in content.
// It returns the block, the start and end byte offsets of the block in content,
// and whether a block was found.
func ParseFirstBlock(content []byte, cfg ParseBlockConfig) (block Block, start int, end int, ok bool) {
	prefix := []byte("---")
	idx := bytes.Index(content, prefix)
	if idx == -1 {
		return
	}
	if idx > 0 && content[idx-1] != '\n' {
		return
	}
	start = idx

	// Extract kind and boundary from the opening line
	lineStart := idx + len(prefix)
	lineEnd := bytes.IndexByte(content[lineStart:], '\n')
	if lineEnd == -1 {
		return
	}
	lineEnd += lineStart
	openingLine := string(content[lineStart:lineEnd])
	parts := strings.SplitN(strings.TrimSpace(openingLine), " ", 2)
	if len(parts) != 2 {
		return
	}
	block.Kind = strings.TrimSpace(parts[0])
	boundary := strings.TrimSpace(parts[1])
	if block.Kind == "" || boundary == "" {
		return
	}
	block.Boundary = boundary

	// Parse headers
	pos := lineEnd + 1
	bodyStart := pos
	block.Headers = make(map[string]string)

	requiredSet := make(map[string]bool)
	for _, h := range cfg.RequiredHeaders {
		requiredSet[strings.ToLower(h)] = false
	}
	requiredCount := len(cfg.RequiredHeaders)
	foundRequired := 0

	knownSet := make(map[string]bool)
	if cfg.KnownHeaders != nil {
		for _, h := range cfg.KnownHeaders {
			knownSet[strings.ToLower(h)] = true
		}
	}
	hasKnown := len(knownSet) > 0

	for pos < len(content) {
		if content[pos] == '\n' {
			// Blank line (just newline)
			pos++
			bodyStart = pos
			break
		}
		headerEnd := bytes.IndexByte(content[pos:], '\n')
		if headerEnd == -1 {
			bodyStart = pos
			break
		}
		headerEnd += pos
		line := strings.TrimSpace(string(content[pos:headerEnd]))

		if line == "" {
			// Whitespace-only line acts as blank line separator
			pos = headerEnd + 1
			bodyStart = pos
			break
		}

		// If all required headers are already collected, stop parsing headers
		if requiredCount > 0 && foundRequired == requiredCount {
			bodyStart = pos
			break
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			// Not a header line; body starts here
			bodyStart = pos
			break
		}

		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		keyLower := strings.ToLower(key)

		// Check if the key is known, if we have a known set
		if hasKnown && !knownSet[keyLower] {
			// Unknown header key, treat as body start
			bodyStart = pos
			break
		}

		if _, exists := block.Headers[keyLower]; !exists {
			block.Headers[keyLower] = val
			if requiredSet[keyLower] == false {
				requiredSet[keyLower] = true
				foundRequired++
			}
		}

		pos = headerEnd + 1
	}

	// Check required headers
	if requiredCount > 0 && foundRequired != requiredCount {
		ok = false
		return
	}

	// Find ---end BOUNDARY marker
	endMarker := "---end " + boundary
	bodyEnd := bytes.Index(content[bodyStart:], []byte(endMarker))
	if bodyEnd == -1 {
		return
	}
	bodyEnd += bodyStart
	if bodyEnd != bodyStart && content[bodyEnd-1] != '\n' {
		return
	}

	// Extract body text
	block.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))

	// Calculate end of entire block
	endLineEnd := bytes.IndexByte(content[bodyEnd:], '\n')
	if endLineEnd == -1 {
		end = len(content)
	} else {
		end = bodyEnd + endLineEnd + 1
	}

	ok = true
	return
}

// Call represents a function call extracted from a "call" block.
type Call struct {
	ID        string
	Function  string
	Arguments map[string]any
	RawBody   string
}

// ParseCalls extracts all call blocks from content and returns parsed Calls.
func ParseCalls(content []byte) ([]Call, error) {
	var calls []Call
	remaining := content
	for {
		block, _, end, ok := ParseFirstBlock(remaining, ParseBlockConfig{
			KnownHeaders:    []string{"function", "id"},
			RequiredHeaders: []string{"function"},
		})
		if !ok {
			break
		}
		if block.Kind == "call" {
			call := Call{
				ID:       block.Headers["id"],
				Function: block.Headers["function"],
				RawBody:  block.Body,
			}
			if block.Body != "" {
				var args map[string]any
				if err := json.Unmarshal([]byte(block.Body), &args); err == nil {
					call.Arguments = args
				}
			}
			calls = append(calls, call)
		}
		// advance past this block and any preceding content
		remaining = remaining[end:]
	}
	return calls, nil
}

