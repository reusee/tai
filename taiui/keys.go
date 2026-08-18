package taiui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const TheoryOfKeyInput = `
taiui key input theory:
- ReadKeys decodes raw terminal input into logical key names. The reader
  is expected to be in non-blocking raw mode: a read that returns 0 bytes
  is polled with a short sleep instead of spinning. A zero-byte read is
  not a completion signal: an escape sequence split across reads is held
  until it completes, and only a partial sequence that receives no
  further bytes within a short grace period is discarded, so a partial
  sequence cannot swallow later input. A lone ESC that never grows is
  emitted as "esc" before it is discarded. ESC followed by a non-sequence
  byte is treated as a stray ESC and the byte is processed normally.
  Standard CSI escape sequences (ESC [ ...), SS3 application cursor
  sequences (ESC O ...), and VT220 tilde sequences (ESC [ n ~) decode the
  arrow keys, home/end, page-up/page-down, tab, digit keys 1-3, bracket
  keys '[' and ']', question-mark '?', s, and q (plus Ctrl-C) into the
  logical names "up", "down", "left", "right", "home", "end", "pageup",
  "pagedown", "tab", "1", "2", "3", "prev-transition", "next-transition",
  "help", "split", "quit", and "esc". An unknown CSI sequence is
  discarded as a whole: its parameter bytes never leak into the key
  stream. The bracket keys name section-transition navigation: a TUI
  jumps its Output pane to the previous or next role or thinking-state
  transition. The question-mark key toggles the operation help overlay.
`

const TheoryOfMouseInput = `
Mouse input theory:
- ReadKeys decodes SGR extended mouse reporting (DECSET 1006), which
  delivers button events (mode 1000) and button-held motion events (mode
  1002) as ESC [ < Cb ; Cx ; Cy M|m sequences. The TUI enables the
  reporting modes on start and disables them on stop (MouseEnableSequence
  and MouseDisableSequence); the parser accepts the sequences whenever
  they arrive, regardless of the terminal's mode settings.
- The button code Cb carries the event flags in bits 5 and 6 (values 32
  and 64): motion or drag events add 32 to the button value, wheel events
  add 64, and the low two bits hold the button number (0 left, 1 middle,
  2 right; 3 means no button). Wire coordinates are 1-based and converted
  to 0-based cell coordinates on emission.
- Each event is emitted as a key name carrying its 0-based coordinates:
  "mouse-left@12,34", "mouse-wheel-up@12,34", and the like. A release
  event carries no button number — the SGR release code is always 3 —
  and is emitted as "mouse-release@12,34"; the consumer tracks which
  button the release ends. No-button motion (code 35) and malformed
  sequences are ignored.
- Like keyboard input, a mouse sequence may arrive split across reads;
  the parser waits for the sequence terminator before emitting.
`

// MouseKeyPrefix is the prefix of the key names ReadKeys emits for mouse
// events. Each name is the prefix followed by the event kind and the
// 0-based cell coordinates, e.g. "mouse-left@12,34". Consumers recognize
// mouse keys by the prefix and split the name at the '@' to recover the
// event kind and coordinates. See TheoryOfMouseInput.
//
// MouseEnableSequence switches the terminal into SGR mouse reporting:
// DECSET 1000 reports button events, DECSET 1002 adds button-held motion
// events (drag), and DECSET 1006 switches coordinate reporting to the
// SGR extended form so columns beyond 223 are reported correctly.
// MouseDisableSequence restores the terminal to ordinary input handling.
//
// The motion and wheel flags are bits of the SGR button code: motion or
// drag events add mouseMotionFlag to the button value, and wheel events
// add mouseWheelFlag. See TheoryOfMouseInput.
const (
	MouseKeyPrefix       = "mouse-"
	MouseEnableSequence  = "\x1b[?1000h\x1b[?1002h\x1b[?1006h"
	MouseDisableSequence = "\x1b[?1000l\x1b[?1002l\x1b[?1006l"
	mouseMotionFlag      = 32
	mouseWheelFlag       = 64
)

// escapeSequenceTimeout is the grace period a partial escape sequence is
// held before it is discarded: a zero-byte read is not a completion
// signal, so a sequence split across reads is kept until it completes or
// the timeout expires. A lone ESC that never grows is emitted as "esc"
// when the timeout expires.
const escapeSequenceTimeout = 50 * time.Millisecond

func ReadKeys(r io.Reader, ch chan<- string) {
	var buf [64]byte
	var pending []byte
	lastData := time.Now()
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			lastData = time.Now()
			pending = append(pending, buf[:n]...)
			for len(pending) > 0 {
				if pending[0] == 0x1b {
					if len(pending) == 1 {
						break
					}
					if pending[1] == 'O' {
						// SS3 application cursor mode: ESC O A|B|C|D|H|F
						if len(pending) < 3 {
							break
						}
						switch pending[2] {
						case 'A':
							ch <- "up"
						case 'B':
							ch <- "down"
						case 'C':
							ch <- "right"
						case 'D':
							ch <- "left"
						case 'H':
							ch <- "home"
						case 'F':
							ch <- "end"
						}
						pending = pending[3:]
						continue
					}
					if pending[1] != '[' {
						// ESC followed by a non-sequence byte: the ESC
						// is not part of an escape sequence.
						pending = pending[1:]
						continue
					}
					if len(pending) < 3 {
						break
					}
					if pending[2] == '<' {
						// SGR mouse sequence: ESC [ < Cb ; Cx ; Cy M|m.
						// The M terminator marks a press, drag, or wheel
						// event; the m terminator marks a release.
						// See TheoryOfMouseInput.
						end := -1
						release := false
						for i := 3; i < len(pending); i++ {
							if pending[i] == 'M' {
								end = i
								break
							}
							if pending[i] == 'm' {
								end = i
								release = true
								break
							}
						}
						if end < 0 {
							// The terminator has not arrived yet; wait for
							// more input.
							break
						}
						payload := string(pending[3:end])
						parts := strings.Split(payload, ";")
						if len(parts) == 3 {
							var button, x, y int
							var parseErr error
							if button, parseErr = strconv.Atoi(parts[0]); parseErr == nil {
								if x, parseErr = strconv.Atoi(parts[1]); parseErr == nil {
									y, parseErr = strconv.Atoi(parts[2])
								}
							}
							if parseErr == nil && x >= 1 && y >= 1 {
								// The SGR protocol sends 1-based coordinates;
								// the TUI uses 0-based cell coordinates.
								if key := mouseKeyName(button, x-1, y-1, release); key != "" {
									ch <- key
								}
							}
						}
						// A malformed or unconsumed mouse sequence is skipped
						// as a whole: dropping only its ESC would leave the
						// rest of the sequence to be misparsed as keys.
						pending = pending[end+1:]
						continue
					}
					// Find the CSI final byte: the first byte in 0x40..0x7E
					// after the '['. Parameter and intermediate bytes are
					// below 0x40, so the scan stops at the final byte.
					end := -1
					for i := 2; i < len(pending); i++ {
						if pending[i] >= 0x40 && pending[i] <= 0x7e {
							end = i
							break
						}
					}
					if end < 0 {
						// The final byte has not arrived yet; wait for more
						// input.
						break
					}
					seq := string(pending[:end+1])
					switch seq {
					case "\x1b[A":
						ch <- "up"
					case "\x1b[B":
						ch <- "down"
					case "\x1b[C":
						ch <- "right"
					case "\x1b[D":
						ch <- "left"
					case "\x1b[H":
						ch <- "home"
					case "\x1b[F":
						ch <- "end"
					case "\x1b[1~":
						ch <- "home"
					case "\x1b[4~":
						ch <- "end"
					case "\x1b[5~":
						ch <- "pageup"
					case "\x1b[6~":
						ch <- "pagedown"
					case "\x1b[7~":
						ch <- "home"
					case "\x1b[8~":
						ch <- "end"
					}
					// An unknown CSI sequence is discarded as a whole: its
					// parameter bytes never leak into the key stream.
					pending = pending[end+1:]
					continue
				}
				switch pending[0] {
				case '1':
					ch <- "1"
				case '2':
					ch <- "2"
				case '3':
					ch <- "3"
				case '[':
					ch <- "prev-transition"
				case ']':
					ch <- "next-transition"
				case '?':
					ch <- "help"
				case 's', 'S':
					ch <- "split"
				case 'q', 'Q', 0x03:
					ch <- "quit"
				case '\t':
					ch <- "tab"
				}
				pending = pending[1:]
			}
		}
		if err != nil {
			// A lone ESC at end of input is a real key press, not a
			// partial sequence: emit it before returning.
			if err == io.EOF && len(pending) == 1 && pending[0] == 0x1b {
				ch <- "esc"
			}
			return
		}
		if n == 0 {
			// The tty is in non-blocking raw mode; avoid a busy loop.
			// A zero-byte read is not a completion signal: a partial
			// escape sequence is held until it completes or the grace
			// period expires, so a sequence split across reads is not
			// lost. A lone ESC that never grows is emitted as "esc"
			// before it is discarded.
			if len(pending) > 0 && pending[0] == 0x1b && time.Since(lastData) > escapeSequenceTimeout {
				if len(pending) == 1 {
					ch <- "esc"
				}
				pending = pending[:0]
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
	}
}

// mouseKeyName maps an SGR mouse button code and event type to the logical
// key name carrying the 0-based cell coordinates, or "" for events that
// carry no usable information (no-button motion in mode 1003). A release
// carries no button number — the SGR release code is always 3 — so every
// release maps to the generic "release" kind; the consumer tracks which
// button the release ends. See TheoryOfMouseInput.
func mouseKeyName(button, x, y int, release bool) string {
	kind := ""
	switch {
	case button&mouseWheelFlag != 0:
		switch button & 3 {
		case 0:
			kind = "wheel-up"
		case 1:
			kind = "wheel-down"
		case 2:
			kind = "wheel-left"
		case 3:
			kind = "wheel-right"
		}
	case release:
		return fmt.Sprintf("%srelease@%d,%d", MouseKeyPrefix, x, y)
	case button&mouseMotionFlag != 0:
		switch button & 3 {
		case 0:
			kind = "leftdrag"
		case 1:
			kind = "middledrag"
		case 2:
			kind = "rightdrag"
		default:
			// Motion without a button (mode 1003) is not consumed.
			return ""
		}
	default:
		switch button & 3 {
		case 0:
			kind = "left"
		case 1:
			kind = "middle"
		case 2:
			kind = "right"
		default:
			// A plain button code of 3 without a release terminator
			// is invalid; drop it.
			return ""
		}
	}
	return fmt.Sprintf("%s%s@%d,%d", MouseKeyPrefix, kind, x, y)
}
