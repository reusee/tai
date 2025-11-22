package generators

import (
	"encoding/json"
	"fmt"
)

type OpenAIParser struct {
	current         *Content
	currentFuncID   string
	currentFuncName string
	currentFuncArgs string
}

func (o *OpenAIParser) Input(delta ChatCompletionStreamChoiceDelta) (ret []*Content, err error) {
	if deltaIsEmpty(delta) {
		if err := o.checkAndEmitCall(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if o.current == nil {
		// new content
		o.current = &Content{
			Role: Role(delta.Role),
		}
	} else if delta.Role != "" && o.current.Role != Role(delta.Role) {
		// role change, new content
		if err := o.checkAndEmitCall(); err != nil {
			return nil, err
		}
		ret = append(ret, o.current)
		o.current = &Content{
			Role: Role(delta.Role),
		}
	}

	if delta.Content != "" {
		if err := o.checkAndEmitCall(); err != nil {
			return nil, err
		}
		o.appendPart(Text(delta.Content))
		lastText := o.current.Parts[len(o.current.Parts)-1].(Text)
		if len(lastText) > 64 {
			ret = append(ret, o.current)
			o.current = &Content{
				Role: o.current.Role,
			}
		}
	}

	if delta.ReasoningContent != "" {
		if err := o.checkAndEmitCall(); err != nil {
			return nil, err
		}
		o.appendPart(Thought(delta.ReasoningContent))
		lastThought := o.current.Parts[len(o.current.Parts)-1].(Thought)
		if len(lastThought) > 64 {
			ret = append(ret, o.current)
			o.current = &Content{
				Role: o.current.Role,
			}
		}
	}

	for _, call := range delta.ToolCalls {
		switch call.Type {

		case "function", "":
			if call.Function.Name != "" {
				// meet new call
				if o.currentFuncName != "" {
					// emit existed
					err := o.checkAndEmitCall()
					if err != nil {
						return nil, err
					}
				}
				o.currentFuncID = call.ID
				o.currentFuncName = call.Function.Name
			}

			if call.Function.Arguments != "" {
				o.currentFuncArgs += call.Function.Arguments
			}

		default:
			panic(fmt.Errorf("unknown tool type: %+v", call))
		}
	}

	return
}

func (o *OpenAIParser) appendPart(part Part) {
	// merge
	if len(o.current.Parts) > 0 {
		prev := o.current.Parts[len(o.current.Parts)-1]
		switch part := part.(type) {
		case Text:
			if text, ok := prev.(Text); ok {
				o.current.Parts[len(o.current.Parts)-1] = text + part
				return
			}
		case Thought:
			if thought, ok := prev.(Thought); ok {
				o.current.Parts[len(o.current.Parts)-1] = thought + part
				return
			}
		}
	}
	o.current.Parts = append(o.current.Parts, part)
}

func (o *OpenAIParser) End() (ret []*Content, err error) {
	if o.currentFuncName != "" {
		err := o.checkAndEmitCall()
		if err != nil {
			return nil, err
		}
		o.currentFuncName = ""
	}
	if o.current != nil {
		ret = append(ret, o.current)
		o.current = nil
	}
	return
}

func (o *OpenAIParser) checkAndEmitCall() error {
	if o.currentFuncName == "" {
		return nil
	}

	if o.current == nil {
		panic("impossible")
	}

	var args map[string]any
	if len(o.currentFuncArgs) > 0 {
		if err := json.Unmarshal([]byte(o.currentFuncArgs), &args); err != nil {
			return err
		}
	} else {
		args = map[string]any{}
	}

	o.current.Parts = append(o.current.Parts, FuncCall{
		ID:   o.currentFuncID,
		Name: o.currentFuncName,
		Args: args,
	})

	o.currentFuncID = ""
	o.currentFuncName = ""
	o.currentFuncArgs = ""

	return nil
}

func deltaIsEmpty(delta ChatCompletionStreamChoiceDelta) bool {
	return delta.Content == "" &&
		delta.Role == "" &&
		len(delta.ToolCalls) == 0 &&
		delta.ReasoningContent == ""
}
