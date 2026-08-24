package generators

// TheoryOfContentUnitSeparation states the separation rule for content
// units assembled into model-facing text. See the constant body for the
// rule and its rationale.
const TheoryOfContentUnitSeparation = `
Content units assembled into model-facing text are separated by a blank
line, never a single newline. Distinct units — file context blocks, prompt
constants, restate prompts, command outputs, fetched resources — are
complete paragraphs, and a single newline fuses the last line of one unit
with the first line of the next into one paragraph, misleading the model
about where each unit begins and ends.

Text parts of the same role are concatenated verbatim: Content.Merge joins
adjacent Text parts without inserting a separator, and the OpenAI message
assembly does the same, because streaming increments must join without
modification. Separation is therefore a construction-time obligation of
the part producer: every complete unit ends its text with a blank line
(\n\n) so the following unit starts a fresh paragraph. Producers of
multi-unit content — the parts providers, the block processors, the prompt
assemblers — follow this rule; the component prompt assembly
(components.ComponentSet.PromptSections, which trims each section and
joins with a blank line) is the reference implementation.
`

type Content struct {
	Role  Role
	Parts []Part
}

func (c Content) Merge(c2 *Content) (*Content, bool) {
	if c.Role != c2.Role {
		// different role
		return nil, false
	}

	var parts []Part
	mergePart := func(part Part) (merge bool) {
		if len(parts) == 0 {
			return false
		}
		prev := parts[len(parts)-1]
		switch prev := prev.(type) {
		case Text:
			if text, ok := part.(Text); ok {
				parts[len(parts)-1] = prev + text
				return true
			}
		case Thought:
			if thought, ok := part.(Thought); ok {
				parts[len(parts)-1] = prev + thought
				return true
			}
		}
		return false
	}

	for _, part := range c.Parts {
		merged := mergePart(part)
		if !merged {
			parts = append(parts, part)
		}
	}
	for _, part := range c2.Parts {
		merged := mergePart(part)
		if !merged {
			parts = append(parts, part)
		}
	}

	return &Content{
		Role:  c.Role,
		Parts: parts,
	}, true
}
