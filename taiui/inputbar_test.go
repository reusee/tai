package taiui

import "testing"

func TestInputBarHandleKeyEdits(t *testing.T) {
	var bar InputBar
	for _, key := range []string{"h", "i"} {
		if !bar.HandleKey(key) {
			t.Fatalf("key %q must be consumed as input", key)
		}
	}
	if got := bar.Line(); got != "hi" {
		t.Fatalf("got line %q, want %q", got, "hi")
	}
	if bar.Cursor() != 2 {
		t.Fatalf("got cursor %d, want 2", bar.Cursor())
	}

	// Insertion at the cursor shifts the rest of the line.
	bar.HandleKey("left")
	bar.HandleKey("o")
	if got := bar.Line(); got != "hoi" {
		t.Fatalf("got line %q, want %q", got, "hoi")
	}
	bar.HandleKey("backspace")
	if got := bar.Line(); got != "hi" {
		t.Fatalf("got line %q, want %q", got, "hi")
	}
	bar.HandleKey("delete")
	if got := bar.Line(); got != "h" {
		t.Fatalf("got line %q, want %q", got, "h")
	}
	bar.HandleKey("home")
	bar.Insert('l')
	if got := bar.Line(); got != "lh" {
		t.Fatalf("got line %q, want %q", got, "lh")
	}
	// ctrl-a and ctrl-e mirror home and end.
	bar.HandleKey("ctrl-e")
	if bar.Cursor() != 2 {
		t.Fatalf("ctrl-e must move the cursor to the end, got %d", bar.Cursor())
	}
	bar.HandleKey("ctrl-a")
	if bar.Cursor() != 0 {
		t.Fatalf("ctrl-a must move the cursor home, got %d", bar.Cursor())
	}
	bar.HandleKey("end")
	bar.HandleKey("space")
	if got := bar.Line(); got != "lh " {
		t.Fatalf("got line %q, want %q", got, "lh ")
	}

	// Non-editing keys fall through to the application's dispatch.
	for _, key := range []string{"up", "down", "tab", "enter", "esc", "ctrl-c", "pageup"} {
		if bar.HandleKey(key) {
			t.Fatalf("key %q must fall through to the application", key)
		}
	}
}

func TestInputBarReset(t *testing.T) {
	bar := InputBar{Prompt: "ask "}
	bar.HandleKey("x")
	bar.HandleKey("left")
	bar.Reset()
	if bar.Prompt != "" || bar.Line() != "" || bar.Cursor() != 0 {
		t.Fatalf("Reset must clear the bar, got prompt %q line %q cursor %d", bar.Prompt, bar.Line(), bar.Cursor())
	}
}

func TestInputBarElement(t *testing.T) {
	style := InputBarStyle{
		BaseBG:      HexColor(0x101010),
		FocusBG:     HexColor(0x202020),
		FocusedFG:   HexColor(0xffffff),
		UnfocusedFG: HexColor(0x808080),
	}
	bar := InputBar{Prompt: "> "}
	bar.HandleKey("a")
	bar.HandleKey("b")
	bar.HandleKey("left")

	box := Box{Top: 0, Left: 0, Bottom: 4, Right: 20}
	screen := newFakeScreen(20, 4)
	Render(bar.Element(box, true, true, style), screen)
	frame := screen.frames[len(screen.frames)-1]
	// The bar is the bottom row of the box and prefixes the prompt.
	if r := screen.cell(0, 3); r != '>' {
		t.Fatalf("expected the prompt at (0,3), got %v", r)
	}
	// A focused bar carries the terminal cursor after the prompt plus
	// the editing offset within the line, and the pane-focused
	// background.
	if !frame.CursorSet || frame.CursorX != 3 || frame.CursorY != 3 {
		t.Fatalf("expected the cursor at (3,3), got set=%v (%d,%d)", frame.CursorSet, frame.CursorX, frame.CursorY)
	}
	wantR, wantG, wantB := style.FocusBG.RGB()
	if r, g, b := frame.Cells[3*frame.Width+0].Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected the pane-focused background on the bar, got %#x %#x %#x", r, g, b)
	}

	// An unfocused bar renders the same text without a cursor, over the
	// pane-unfocused background.
	unfocused := newFakeScreen(20, 4)
	Render(bar.Element(box, false, false, style), unfocused)
	unfocusedFrame := unfocused.frames[len(unfocused.frames)-1]
	if unfocusedFrame.CursorSet {
		t.Fatal("an unfocused input bar must not carry the terminal cursor")
	}
	wantR, wantG, wantB = style.BaseBG.RGB()
	if r, g, b := unfocusedFrame.Cells[3*unfocusedFrame.Width+0].Style.Bg().RGB(); r != wantR || g != wantG || b != wantB {
		t.Fatalf("expected the pane-unfocused background on the bar, got %#x %#x %#x", r, g, b)
	}

	// An empty prompt renders the default ">> ".
	var defaultPrompt InputBar
	defaultPrompt.HandleKey("z")
	defaults := newFakeScreen(20, 4)
	Render(defaultPrompt.Element(box, true, false, style), defaults)
	if r := defaults.cell(0, 3); r != '>' {
		t.Fatalf("expected the default prompt at (0,3), got %v", r)
	}
}
