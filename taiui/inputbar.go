package taiui

import (
	"unicode"
	"unicode/utf8"
)

// TheoryOfInputBar states the design of the reusable single-line input
// bar; see the constant body.
const TheoryOfInputBar = `
taiui input bar theory:
- InputBar is the reusable single-line input bar: a prompt, a typed
  rune line, and an editing cursor, with the key-driven line editing
  (HandleKey) and the bar rendering (Element) derived from that state.
  It is pure state: no goroutine, no channel, no lock. The caller
  serializes access, typically in its event loop, exactly like Tabs and
  ScrollState.
- The application owns the policy around the bar: when it takes the
  keyboard (focus), when a submitted line is delivered, and which keys
  fall through. HandleKey covers line editing only — inserting a
  printable rune, backspace, delete, left/right, home/end — and returns
  false for every other key (arrows, page keys, tab, enter, esc,
  ctrl-c), so the application's dispatch keeps serving navigation and
  its own submit and release semantics while the bar is focused.
- Rendering is one row: the bar occupies the bottom row of the given
  box, prefixing the line with the prompt (default ">> "). A focused
  bar shows the terminal cursor at the editing position through the
  Input element; an unfocused bar keeps plain text in a dimmer
  foreground. The background follows the containing pane's focus state,
  so the bar blends with the panel above it.
- CursorAt positions within the rendered text, so the prompt shifts the
  editing position by its own rune count.
`

// InputBarStyle styles the input bar. BaseBG is the bar's background
// while the pane containing it is unfocused and FocusBG while it is
// focused, mirroring PanelStyle's two backgrounds; FocusedFG is the
// text color while the bar holds the keyboard, UnfocusedFG while it
// does not.
type InputBarStyle struct {
	BaseBG      Color
	FocusBG     Color
	FocusedFG   Color
	UnfocusedFG Color
}

// InputBar is the state of a single-line input bar: a prompt, the typed
// rune line, and the editing cursor. The line editing (HandleKey,
// Insert) and the bar rendering (Element) are reusable; the application
// owns the focus policy and the submit protocol around them. See
// TheoryOfInputBar.
type InputBar struct {
	// Prompt prefixes the rendered line. An empty prompt renders the
	// default ">> ".
	Prompt string

	line   []rune
	cursor int
}

// Line returns the typed line.
func (b *InputBar) Line() string {
	return string(b.line)
}

// Cursor returns the editing position as a rune offset within the line.
func (b *InputBar) Cursor() int {
	return b.cursor
}

// Reset clears the prompt, the line, and the cursor, returning the bar
// to its zero-value state for the next input round.
func (b *InputBar) Reset() {
	b.Prompt = ""
	b.line = nil
	b.cursor = 0
}

// Insert inserts one rune at the editing cursor, shifting the rest of
// the line.
func (b *InputBar) Insert(r rune) {
	b.line = append(b.line, 0)
	copy(b.line[b.cursor+1:], b.line[b.cursor:])
	b.line[b.cursor] = r
	b.cursor++
}

// HandleKey applies one key to the line editing: backspace, delete,
// left, right, home and ctrl-a, end and ctrl-e, space, and a single
// printable rune inserted at the cursor. It reports whether the key was
// consumed; every other key — arrows, page keys, tab, enter, esc,
// ctrl-c, and multi-rune or non-printable input — returns false so the
// application's dispatch handles it. See TheoryOfInputBar.
func (b *InputBar) HandleKey(key string) bool {
	switch {
	case key == "backspace":
		if b.cursor > 0 {
			b.line = append(b.line[:b.cursor-1], b.line[b.cursor:]...)
			b.cursor--
		}
	case key == "delete":
		if b.cursor < len(b.line) {
			b.line = append(b.line[:b.cursor], b.line[b.cursor+1:]...)
		}
	case key == "left":
		if b.cursor > 0 {
			b.cursor--
		}
	case key == "right":
		if b.cursor < len(b.line) {
			b.cursor++
		}
	case key == "home", key == "ctrl-a":
		b.cursor = 0
	case key == "end", key == "ctrl-e":
		b.cursor = len(b.line)
	case key == "space":
		b.Insert(' ')
	default:
		// ReadKeys emits printable ASCII and decoded multi-byte
		// characters (CJK, emoji) as the character itself, so a key that
		// is one printable rune inserts at the cursor — keys that double
		// as navigation bindings in an application (q, 1..3, s, [, ])
		// are typed as text. Anything else falls through.
		if r, size := utf8.DecodeRuneInString(key); len(key) > 0 && size == len(key) && unicode.IsPrint(r) {
			b.Insert(r)
			return true
		}
		return false
	}
	return true
}

// Element renders the bar as the bottom row of box: the prompt and the
// typed line, with the terminal cursor at the editing position while
// focused. focused reports whether the bar holds the keyboard;
// paneFocused reports whether the pane containing the bar holds the
// focus, which selects the bar's background so it blends with the panel
// above it. See TheoryOfInputBar.
func (b *InputBar) Element(box Box, focused, paneFocused bool, style InputBarStyle) Element {
	prompt := b.Prompt
	if prompt == "" {
		prompt = ">> "
	}
	text := prompt + string(b.line)
	var input Element
	if focused {
		input = Input(text, utf8.RuneCountInString(prompt)+b.cursor, FGColor(style.FocusedFG))
	} else {
		input = Text(text, FGColor(style.UnfocusedFG))
	}
	bg := style.BaseBG
	if paneFocused {
		bg = style.FocusBG
	}
	return Rect(
		Box{Top: box.Bottom - 1, Left: box.Left, Bottom: box.Bottom, Right: box.Right},
		Fill(true),
		BGColor(bg),
		input,
	)
}
