package generators

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
