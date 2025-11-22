package generators

import (
	"errors"
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

	// color
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

	separated := false
	print := func(isThought bool, str string) (err error) {
		if roleColor != "" {
			if _, err := fmt.Fprint(s.w, roleColor); err != nil {
				return err
			}
			defer func() {
				_, e := fmt.Fprint(s.w, ColorReset)
				err = errors.Join(err, e)
			}()
		}

		// separate
		if !separated &&
			s.lastOutputRole != "" &&
			s.lastOutputRole != content.Role {
			if _, err := fmt.Fprint(s.w, "\n\n"); err != nil {
				return err
			}
			separated = true
		}

		// think mark
		if !ret.lastOutputIsThought && isThought {
			// open
			if _, err := fmt.Fprint(s.w, "<think>\n"); err != nil {
				return err
			}
			ret.lastOutputIsThought = true
		}
		if ret.lastOutputIsThought && !isThought {
			// close
			if _, err := fmt.Fprint(s.w, "\n</think>\n"); err != nil {
				return err
			}
			ret.lastOutputIsThought = false
		}

		if _, err := fmt.Fprint(s.w, str); err != nil {
			return err
		}

		ret.lastOutputRole = content.Role
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
