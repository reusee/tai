package taiui

import "reflect"

// Spec is the language of element construction: any value that can appear
// in an element's spec list. Style and layout specs, elements themselves,
// and Specs groups all implement Spec, so spec lists compose and nest.
// Bare strings are accepted by Text as a shorthand for lines, but they are
// not Specs; a concrete Style value is likewise accepted directly without a
// marker because Style aliases an external type.
type Spec interface {
	spec()
}

// Spec marker methods are grouped here: they are part of the Spec protocol,
// not of the declaring type's own design.
func (Box) spec()   {}
func (Align) spec() {}

func (VAlign) spec()  {}
func (BGColor) spec() {}
func (FGColor) spec() {}

func (UnderlineColor) spec() {}
func (Bold) spec()           {}
func (Underline) spec()      {}

func (UnderlineStyle) spec()  {}
func (Fill) spec()            {}
func (StyleFunc) spec()       {}
func (_Margin) spec()         {}
func (_Padding) spec()        {}
func (_Rect) spec()           {}
func (_Text) spec()           {}
func (_VerticalScroll) spec() {}
func (_FrameBuffer) spec()    {}

func (_Overlay) spec() {}

func (_List) spec() {}

func (Border) spec() {}

func (BorderType) spec() {}

func (BorderStyle) spec() {}

func (Title) spec() {}

// Specs is a group of specs. It is itself a Spec, so groups nest and can
// be placed anywhere a single Spec is accepted.
type Specs []Spec

func (Specs) spec() {}

// If composes specs conditionally: it returns a group of the given specs
// when cond is true, and no specs otherwise.
func If(cond bool, specs ...Spec) Spec {
	if cond {
		return Specs(specs)
	}
	return nil
}

// Alt selects one of two specs: it returns ifTrue when cond is true, and
// ifFalse otherwise.
func Alt(cond bool, ifTrue, ifFalse Spec) Spec {
	if cond {
		return ifTrue
	}
	return ifFalse
}

// elementBase holds the specs common to every element: an optional Box
// override, fill behavior, and an ordered style chain. Elements interpret
// these at construction time; rendering reads the fields.
type elementBase struct {
	box    *Box
	fill   bool
	styles []StyleFunc
}

func (b *elementBase) applyCommonSpec(spec any) bool {
	switch spec := spec.(type) {
	case Box:
		b.box = &spec
	case Fill:
		b.fill = bool(spec)
	case Style:
		b.styles = append(b.styles, func(s Style) Style { return spec })
	case StyleFunc:
		b.styles = append(b.styles, spec)
	case FGColor:
		b.styles = append(b.styles, SameStyle.SetFG(Color(spec)))
	case BGColor:
		b.styles = append(b.styles, SameStyle.SetBG(Color(spec)))
	case UnderlineColor:
		b.styles = append(b.styles, SameStyle.SetUnderlineColor(Color(spec)))
	case UnderlineStyle:
		b.styles = append(b.styles, SameStyle.SetUnderlineStyle(spec))
	case Bold:
		b.styles = append(b.styles, SameStyle.SetBold(bool(spec)))
	case Blink:
		b.styles = append(b.styles, SameStyle.SetBlink(bool(spec)))
	case Dim:
		b.styles = append(b.styles, SameStyle.SetDim(bool(spec)))
	case Italic:
		b.styles = append(b.styles, SameStyle.SetItalic(bool(spec)))
	case Reverse:
		b.styles = append(b.styles, SameStyle.SetReverse(bool(spec)))
	case Overline:
		b.styles = append(b.styles, SameStyle.SetOverline(bool(spec)))
	case StrikeThrough:
		b.styles = append(b.styles, SameStyle.SetStrikeThrough(bool(spec)))
	case Underline:
		b.styles = append(b.styles, SameStyle.SetUnderline(bool(spec)))
	default:
		return false
	}
	return true
}

func (b *elementBase) effectiveBox(fallback Box) Box {
	if b.box != nil {
		return *b.box
	}
	return fallback
}

func (b *elementBase) styled(style Style) Style {
	for _, fn := range b.styles {
		style = fn(style)
	}
	return style
}

// specApplier is implemented by elements that are built from spec lists: it
// interprets one spec value into typed fields. Interpretation happens at
// construction, so unknown specs fail immediately and rendering never parses
// specs.
type specApplier interface {
	applySpec(spec any)
}

// buildElement resolves the spec list and applies each spec to the element.
func buildElement(e specApplier, specs []any) {
	for _, spec := range resolveSpecs(specs) {
		e.applySpec(spec)
	}
}

func resolveSpecs(specs []any) []any {
	var out []any
	for _, spec := range specs {
		if spec == nil {
			continue
		}
		t := reflect.TypeOf(spec)
		if t == nil {
			// a typed-nil Spec, e.g. If(false, ...) outside a group
			continue
		}
		if t.Kind() == reflect.Func && t.NumIn() == 0 {
			v := reflect.ValueOf(spec)
			if v.IsNil() {
				// a nil zero-argument function spec is treated like nil
				continue
			}
			res := v.Call(nil)
			for _, r := range res {
				out = append(out, resolveSpecs([]any{r.Interface()})...)
			}
			continue
		}
		out = append(out, spec)
	}
	return out
}
