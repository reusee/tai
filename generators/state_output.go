package generators

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

type Output struct {
	upstream            State
	w                   io.Writer
	isTerminal          bool
	showThoughts        bool
	lastOutputRole      Role
	lastOutputIsThought bool

	disableThoughts bool
	disableTools    bool
}

const Theory = `
# Output State

The Output state is a decorator that provides real-time visual feedback for the generation process by writing to an io.Writer.

## Conceptual Integrity of the Visual Stream

The visual stream must remain coherent even when content is delivered in small, irregular chunks (streaming). This requires careful management of state transitions between different roles and between thought/normal text.

1. **Role Separation**: When the role changes between consecutive content blocks, a visual separator (dual newline) is inserted to distinguish them.
2. **Thinking Tags**: Reasoning models often provide "thought" parts. These are wrapped in <think> tags. The state must track whether a thinking block is currently open to ensure tags are correctly balanced across multiple AppendContent calls.
3. **Coloring**: In terminal environments, different roles are assigned different colors. Color escape codes must be applied and reset correctly for each chunk to avoid color bleeding into subsequent terminal output. Thoughts use a specific ColorThought to distinguish them from model responses.
4. **Flush**: The Flush operation marks the end of a generation sequence. It must ensure all open tags (like <think>) are closed and provide final spacing.
`

func NewOutput(upstream State, w io.Writer, showThoughts bool) Output {
	isTerminal := false
	if file, ok := w.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		isTerminal = true
	}
	return Output{
		upstream:     upstream,
		w:            w,
		isTerminal:   isTerminal,
		showThoughts: showThoughts,
	}
}

func (s Output) WithThoughts(yes bool) Output {
	s.disableThoughts = !yes
	return s
}

func (s Output) WithTools(yes bool) Output {
	s.disableTools = !yes
	return s
}

var _ State = Output{}

func (s Output) AppendContent(content *Content) (_ State, err error) {
	ret := s // copy

	// Determine color
	var roleColor string
	if s.isTerminal {
		switch content.Role {
		case RoleUser:
			roleColor = ColorUser
		case RoleModel, RoleAssistant:
			roleColor = ColorReset
		case RoleTool:
			roleColor = ColorTool
		case RoleSystem:
			roleColor = ColorSystem
		case RoleLog:
			roleColor = ColorLog
		}
	}

	// Role change separation and thought closing
	if s.lastOutputRole != "" && s.lastOutputRole != content.Role {
		// If we were in a thought from the previous role, close it now
		if ret.lastOutputIsThought {
			if _, err := fmt.Fprint(s.w, "\n</think>\n"); err != nil {
				return nil, err
			}
			ret.lastOutputIsThought = false
		}
		// Separation
		if _, err := fmt.Fprint(s.w, "\n\n"); err != nil {
			return nil, err
		}
	}

	print := func(isThought bool, str string) (err error) {
		// Transition thought state within the current content
		if !ret.lastOutputIsThought && isThought {
			if _, err := fmt.Fprint(s.w, "<think>\n"); err != nil {
				return err
			}
			ret.lastOutputIsThought = true
		} else if ret.lastOutputIsThought && !isThought {
			if _, err := fmt.Fprint(s.w, "\n</think>\n"); err != nil {
				return err
			}
			ret.lastOutputIsThought = false
		}

		// Apply color
		c := roleColor
		if isThought && s.isTerminal {
			c = ColorThought
		}
		if c != "" {
			if _, err := fmt.Fprint(s.w, c); err != nil {
				return err
			}
		}

		// Output content
		if _, err := fmt.Fprint(s.w, str); err != nil {
			return err
		}

		// Reset color
		if c != "" {
			if _, err := fmt.Fprint(s.w, ColorReset); err != nil {
				return err
			}
		}

		return nil
	}

	for _, part := range content.Parts {

		switch part := part.(type) {

		case Text:
			if err := print(false, string(part)); err != nil {
				return nil, err
			}

		case Thought:
			if ret.showThoughts && !ret.disableThoughts {
				if err := print(true, string(part)); err != nil {
					return nil, err
				}
			}

		case FileURL:
			if err := print(false, fmt.Sprintf("[File: %s]", part)); err != nil {
				return nil, err
			}

		case FileContent:
			if err := print(false, fmt.Sprintf("[File Content: %s]", part.MimeType)); err != nil {
				return nil, err
			}

		case FuncCall:
			if !ret.disableTools {
				if err := print(false, fmt.Sprintf("[Function Call: %s(%v)]", part.Name, part.Args)); err != nil {
					return nil, err
				}
			}

		case CallResult:
			if !ret.disableTools {
				if err := print(false, fmt.Sprintf("[Call Result: %s(%v)]", part.Name, part.Results)); err != nil {
					return nil, err
				}
			}

		case FinishReason:
			if err := print(false, fmt.Sprintf("[Finish: %s]", part)); err != nil {
				return nil, err
			}

		case Error:
			if err := print(false, fmt.Sprintf("[Error: %v]", part)); err != nil {
				return nil, err
			}

		}
	}

	ret.lastOutputRole = content.Role
	ret.upstream, err = s.upstream.AppendContent(content)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (s Output) Contents() []*Content {
	return s.upstream.Contents()
}

func (s Output) FuncMap() map[string]*Func {
	return s.upstream.FuncMap()
}

func (s Output) SystemPrompt() string {
	return s.upstream.SystemPrompt()
}

func (s Output) Flush() (State, error) {
	ret := s // copy
	if ret.lastOutputIsThought {
		if _, err := io.WriteString(s.w, "\n</think>\n"); err != nil {
			return nil, err
		}
		ret.lastOutputIsThought = false
	}
	if _, err := io.WriteString(s.w, "\n\n"); err != nil {
		return nil, err
	}
	var err error
	ret.upstream, err = s.upstream.Flush()
	if err != nil {
		return nil, err
	}
	ret.lastOutputRole = ""
	return ret, nil
}

func (s Output) Unwrap() State {
	return s.upstream
}

