package taiplay

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const Theory = `
# S-Expression Editing Theory

1. Structural Targeting: Unlike line-based diffs, we target S-expressions within tai.playbook. An S-expression is the fundamental unit of state in taiplay.
2. Prefix Matching: A target is identified by a prefix. The tool searches for a unique top-level S-expression that starts with this prefix. This allows the AI to specify a target concisely.
3. Operations (always targeting tai.playbook):
   - S-MODIFY: Replaces the entire targeted S-expression with new content.
   - S-DELETE: Removes the targeted S-expression.
   - S-ADD_BEFORE: Inserts new S-expression(s) before the target.
   - S-ADD_AFTER: Inserts new S-expression(s) after the target.
4. Uniqueness Constraint: To prevent ambiguous edits, the prefix must match exactly one top-level S-expression in the file.
5. TVM Compatibility: This mechanism is designed to update Lisp-formatted Playbook files while maintaining syntactic integrity.
6. Boundary Anchors: 'BEGIN' and 'END' can be used as targets to insert content at the very start or end of tai.playbook, which is essential for initializing new Playbooks or appending to existing ones.
`

var headerRegexp = regexp.MustCompile(`(?m)^(\s*)\[\[\[ (S-MODIFY|S-ADD_BEFORE|S-ADD_AFTER|S-DELETE) (.*)`)

const playbookFile = "tai.playbook"

type Hunk struct {
	Op           string
	TargetPrefix string
	Body         string
}

func ApplySexprPatches(root *os.Root, content []byte) (bool, error) {
	hunks := parseHunks(content)
	if len(hunks) == 0 {
		return false, nil
	}
	for _, h := range hunks {
		if err := applyHunk(root, h); err != nil {
			return true, fmt.Errorf("apply %s on %s: %w", h.Op, h.TargetPrefix, err)
		}
	}
	return true, nil
}

func parseHunks(content []byte) []Hunk {
	var hunks []Hunk
	matches := headerRegexp.FindAllSubmatchIndex(content, -1)
	for i, m := range matches {
		hunk := Hunk{
			Op:           string(content[m[4]:m[5]]),
			TargetPrefix: strings.TrimSpace(string(content[m[6]:m[7]])),
		}
		start := m[1]
		endOfHeader := bytes.IndexByte(content[start:], '\n')
		if endOfHeader == -1 {
			continue
		}
		bodyStart := start + endOfHeader + 1
		bodyEnd := len(content)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		footerIdx := bytes.Index(content[bodyStart:bodyEnd], []byte("]]]"))
		if footerIdx != -1 {
			bodyEnd = bodyStart + footerIdx
		}
		hunk.Body = strings.TrimSpace(string(content[bodyStart:bodyEnd]))
		hunks = append(hunks, hunk)
	}
	return hunks
}

func applyHunk(root *os.Root, h Hunk) error {
	src, err := root.ReadFile(playbookFile)
	if err != nil {
		if os.IsNotExist(err) && (h.Op == "S-ADD_BEFORE" || h.Op == "S-ADD_AFTER" || h.Op == "S-MODIFY") {
			return root.WriteFile(playbookFile, []byte(h.Body), 0644)
		}
		return err
	}

	start, end, err := findTargetRange(src, h.TargetPrefix)
	if err != nil {
		if h.Op == "S-MODIFY" || h.Op == "S-DELETE" {
			return nil // No-op for missing target in modify/delete
		}
		start, end = len(src), len(src) // Append for add ops if target missing
	}

	var newSrc []byte
	switch h.Op {
	case "S-MODIFY":
		newSrc = append(src[:start], append([]byte(h.Body), src[end:]...)...)
	case "S-DELETE":
		newSrc = append(src[:start], src[end:]...)
	case "S-ADD_BEFORE":
		newSrc = append(src[:start], append([]byte(h.Body+"\n\n"), src[start:]...)...)
	case "S-ADD_AFTER":
		newSrc = append(src[:end], append([]byte("\n\n"+h.Body), src[end:]...)...)
	}

	return root.WriteFile(playbookFile, bytes.TrimSpace(newSrc), 0644)
}

func findTargetRange(src []byte, prefix string) (int, int, error) {
	if prefix == "BEGIN" {
		return 0, 0, nil
	}
	if prefix == "END" {
		return len(src), len(src), nil
	}
	sexprs := scanTopLevelSexprs(src)
	var found []sexprPos
	for _, s := range sexprs {
		content := string(src[s.start:s.end])
		if strings.HasPrefix(content, prefix) {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return 0, 0, fmt.Errorf("target sexpr prefix not found: %s", prefix)
	}
	if len(found) > 1 {
		return 0, 0, fmt.Errorf("ambiguous target sexpr prefix: %s", prefix)
	}
	return found[0].start, found[0].end, nil
}

type sexprPos struct {
	start int
	end   int
}

func scanTopLevelSexprs(src []byte) []sexprPos {
	var res []sexprPos
	for i := 0; i < len(src); {
		if src[i] <= ' ' {
			i++
			continue
		}
		if src[i] == '(' {
			start := i
			depth := 0
			inString := false
			escaped := false
			for j := i; j < len(src); j++ {
				if escaped {
					escaped = false
					continue
				}
				if src[j] == '\\' {
					escaped = true
					continue
				}
				if src[j] == '"' {
					inString = !inString
					continue
				}
				if !inString {
					if src[j] == '(' {
						depth++
					} else if src[j] == ')' {
						depth--
						if depth == 0 {
							res = append(res, sexprPos{start: start, end: j + 1})
							i = j + 1
							goto next
						}
					}
				}
			}
			break
		}
		i++
	next:
	}
	return res
}