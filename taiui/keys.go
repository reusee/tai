package taiui

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const TheoryOfKeyInput = `
taiui key input theory:
- Raw terminal input is decoded in non-blocking raw mode into logical
  key names. A zero-byte read is not completion: an incomplete escape
  sequence split across reads is held until it completes or a grace
  period expires. A lone escape that never grows is emitted as "esc".
- An escape followed by a printable character is "alt-<char>"; an
  escape followed by a control byte is "alt-<key>", so Alt+Enter is
  "alt-enter" and Alt+Ctrl+A is "alt-ctrl-a". An escape followed by an
  escape drops the first escape. Multi-byte UTF-8 input decodes to its
  rune and is emitted as the character, so accented characters, CJK,
  and emoji reach the application as decoded characters; an escape
  followed by a multi-byte character is the alt form of that character.
  An incomplete multi-byte sequence that never completes within the
  grace period is dropped.
- CSI, SS3, and tilde sequences decode arrows, home/end, page-up/down,
  insert/delete, function keys, shift-tab, focus events, and
  bracketed-paste markers. A modified cursor-position report is
  deliberately not decoded as a key, to avoid ambiguity with position
  reports. Unknown sequences are discarded whole. The Linux console's
  non-standard function-key encoding is recognized as a specific
  pattern before the generic sequence scan.
- Control string sequences (OSC, DCS, SOS, PM, APC) carry terminal
  metadata that cannot be conveyed as key names, so they are consumed
  and discarded. The trade-off is that alt-prefixed combinations using
  their introducers are not reported as key events; applications
  needing those combinations should use the Kitty keyboard protocol,
  which disambiguates them.
- The Kitty keyboard protocol maps its key codes to the same logical
  names, with release events ignored. Caps-lock and num-lock are lock
  states, not transient modifiers, so they are not part of the key
  prefix. Kitty's modifier bit assignment differs from xterm's and is
  translated. Mode-setting and query-response sequences are consumed
  silently.
- Keypad keys decode under both the Kitty and the SS3 application
  keypad encodings.
- Single bytes cover the control and printable ranges. Key names are
  generic, not application-specific, so the library stays reusable
  across projects; the application maps key names to actions.
`

const TheoryOfMouseInput = `
taiui mouse input theory:
- ReadKeys decodes three mouse reporting formats: SGR extended (DECSET
  1006), URXVT (DECSET 1015), and X10 (DECSET 9). SGR delivers button
  events and button-held motion as ESC [ < Cb ; Cx ; Cy M|m; URXVT
  delivers them as ESC [ Cb ; Cx ; Cy M with the button code offset by
  32; X10 delivers them as ESC [ M followed by 3 raw bytes (button+32,
  x+32, y+32). The parser accepts any format whenever it arrives,
  regardless of the terminal's mode settings. SGR pixel mode (DECSET
  1016) uses the same SGR format with pixel-level coordinates; the
  reader passes coordinates through unchanged, so the application
  converts pixel coordinates to cells when it has enabled pixel mode.
- The button code Cb carries the event flags and modifiers: bits 2-4
  (values 4, 8, 16) carry Shift, Alt, and Ctrl modifiers; bits 5 and 6
  (values 32 and 64) carry the motion/drag and wheel flags; bit 7
  (value 128) marks an extended button (buttons 8-11, encoded in the
  low two bits as 0-3); the low two bits hold the button number (0 left,
  1 middle, 2 right; 3 means no button). Wire coordinates are 1-based
  and converted to 0-based cell coordinates on emission. In X10 and
  URXVT formats, the raw button byte includes the +32 offset; the
  parser subtracts it.
- Each event is emitted as a key name carrying its 0-based coordinates:
  "mouse-left@12,34", "mouse-wheel-up@12,34", "mouse-button-8@12,34",
  and the like. Extended buttons (8-11) are reported as "button-8"
  through "button-11"; extended button drag appends "-drag". Keyboard
  modifiers (Shift, Alt, Ctrl) are extracted from the button code and
  prepended as a dash-joined prefix, matching the keyboard modifier
  convention: "shift-mouse-left@12,34", "ctrl-mouse-wheel-up@12,34". A
  release event carries no button number — the SGR release code is
  always 3 — and is emitted as "mouse-release@12,34" with any detected
  modifier prefix; the consumer tracks which button the release ends.
  In X10 and URXVT, the low 2 bits set to 3 (after offset subtraction)
  indicates release, regardless of modifier bits. No-button motion
  (code 35) and malformed sequences are ignored.
- Like keyboard input, a mouse sequence may arrive split across reads;
  the parser waits for the sequence terminator (or all 3 raw bytes for
  X10) before emitting.
`

const (
	MouseKeyPrefix       = "mouse-"
	MouseEnableSequence  = "\x1b[?1000h\x1b[?1002h\x1b[?1006h"
	MouseDisableSequence = "\x1b[?1000l\x1b[?1002l\x1b[?1006l"
	mouseMotionFlag      = 32
	mouseWheelFlag       = 64
	mouseShiftFlag       = 4
	mouseAltFlag         = 8
	mouseCtrlFlag        = 16
)

// escapeSequenceTimeout is the grace period a partial escape sequence is
// held before it is discarded: a zero-byte read is not a completion
// signal, so a sequence split across reads is kept until it completes or
// the timeout expires. A lone ESC that never grows is emitted as "esc"
// when the timeout expires.
const escapeSequenceTimeout = 50 * time.Millisecond

const (
	// BracketedPasteEnableSequence switches the terminal into bracketed
	// paste mode (DECSET 2004): pasted text is wrapped in ESC [ 200 ~ and
	// ESC [ 201 ~ markers. ReadKeys decodes them as "paste-start" and
	// "paste-end" keys.
	BracketedPasteEnableSequence  = "\x1b[?2004h"
	BracketedPasteDisableSequence = "\x1b[?2004l"

	// FocusReportingEnableSequence switches the terminal into focus
	// reporting mode (DECSET 1004): focus gain and loss are reported as
	// ESC [ I and ESC [ O. ReadKeys decodes them as "focus-in" and
	// "focus-out" keys.
	FocusReportingEnableSequence  = "\x1b[?1004h"
	FocusReportingDisableSequence = "\x1b[?1004l"

	// KittyKeyboardEnableSequence pushes the Kitty keyboard protocol onto
	// the terminal's mode stack (CSI > 1 u, discrete mode). Keys arrive as
	// CSI code;mod[;event] u sequences. KittyKeyboardDisableSequence pops
	// the stack (CSI < 0 u). ReadKeys decodes both key events and
	// mode-setting sequences; the latter are consumed silently.
	KittyKeyboardEnableSequence  = "\x1b[>1u"
	KittyKeyboardDisableSequence = "\x1b[<0u"
)

func ReadKeys(r io.Reader, ch chan<- string) {
	var buf [64]byte
	var pending []byte
	lastData := time.Now()
	for {
		n, err := r.Read(buf[:])
		if n > 0 {
			lastData = time.Now()
			pending = append(pending, buf[:n]...)
			pending = processKeys(pending, ch)
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
			// before it is discarded. An incomplete multi-byte UTF-8
			// sequence is likewise dropped after the grace period.
			if len(pending) > 0 && time.Since(lastData) > escapeSequenceTimeout {
				if pending[0] == 0x1b {
					if len(pending) == 1 {
						ch <- "esc"
					}
					pending = pending[:0]
				} else if pending[0] >= 0x80 {
					// An incomplete multi-byte UTF-8 sequence that
					// never grew to a complete rune: drop it.
					pending = pending[:0]
				}
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
	}
}

func processKeys(pending []byte, ch chan<- string) []byte {
	for len(pending) > 0 {
		if pending[0] == 0x1b {
			if len(pending) == 1 {
				return pending
			}
			if pending[1] == 'O' {
				// SS3 application cursor mode. Standard SS3 keys are
				// ESC O <final>, but modified SS3 keys carry
				// parameters (e.g., ESC O 1;2A for shift-up in
				// application cursor mode). When the byte after O is
				// a parameter byte (0x30-0x3F), scan for the final
				// byte as with CSI; otherwise the next byte is the
				// final byte of a standard SS3 key.
				if len(pending) < 3 {
					return pending
				}
				if pending[2] >= 0x30 && pending[2] <= 0x3f {
					end := -1
					for i := 3; i < len(pending); i++ {
						if pending[i] >= 0x40 && pending[i] <= 0x7e {
							end = i
							break
						}
					}
					if end < 0 {
						return pending
					}
					emitSS3KeyWithMods(string(pending[2:end]), string(pending[end]), ch)
					pending = pending[end+1:]
					continue
				}
				emitSS3Key(pending[2], ch)
				pending = pending[3:]
				continue
			}
			if pending[1] == '[' {
				if len(pending) < 3 {
					return pending
				}
				// Linux console function keys: ESC [ [ A-E (F1-F5),
				// ESC [ [ a-e (Shift+F1-F5). The second [ is not a
				// standard CSI parameter byte, so these sequences are
				// recognized before the generic CSI scan.
				if len(pending) >= 4 && pending[2] == '[' {
					if key := linuxConsoleKey(pending[3]); key != "" {
						ch <- key
						pending = pending[4:]
						continue
					}
				}
				if pending[2] == '<' {
					// SGR mouse sequence: ESC [ < Cb ; Cx ; Cy M|m.
					// A CSI < ... u is a Kitty keyboard mode pop, not
					// a mouse sequence; handleSGRMouse returns -2 and
					// the code falls through to the CSI scan below.
					consumed := handleSGRMouse(pending, ch)
					if consumed == -2 {
						// Not a mouse sequence: handle as a regular CSI.
					} else {
						if consumed < 0 {
							return pending
						}
						pending = pending[consumed:]
						continue
					}
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
					return pending
				}
				// X10 mouse: \x1b[M followed by 3 raw bytes (button+32,
				// x+32, y+32). M is the CSI final byte with no
				// parameters, and 3 data bytes follow it outside the
				// CSI syntax. No standard CSI key uses final byte M
				// with empty parameters, so this is unambiguous.
				if end == 2 && pending[end] == 'M' {
					if len(pending) < end+4 {
						return pending // need all 3 data bytes
					}
					handleX10Mouse(pending[end+1:end+4], ch)
					pending = pending[end+4:]
					continue
				}
				emitCSIKey(string(pending[2:end]), string(pending[end]), ch)
				pending = pending[end+1:]
				continue
			}
			// Control string sequences (OSC, DCS, SOS, PM, APC) start
			// with ESC followed by ], P, X, ^, or _, and end with BEL
			// (0x07) or ST (ESC \). They carry terminal metadata (window
			// titles, clipboard, color queries) that ReadKeys cannot
			// convey as key names, so they are consumed and discarded.
			// The trade-off is that Alt+], Alt+P, Alt+X, Alt+^, and Alt+_
			// are not reported as key events; applications needing those
			// combinations should use the Kitty keyboard protocol, which
			// disambiguates them.
			if pending[1] == ']' || pending[1] == 'P' || pending[1] == 'X' || pending[1] == '^' || pending[1] == '_' {
				consumed := consumeControlString(pending)
				if consumed < 0 {
					return pending
				}
				pending = pending[consumed:]
				continue
			}
			// ESC followed by a multi-byte UTF-8 character is
			// Alt+character, matching the single-byte Alt+key rule.
			if pending[1] >= 0x80 {
				consumed, key := decodeUTF8Key(pending[1:])
				if consumed == 0 {
					return pending
				}
				if key != "" {
					ch <- "alt-" + key
				}
				pending = pending[1+consumed:]
				continue
			}
			// ESC followed by a printable character is Alt+key.
			if pending[1] >= 0x20 && pending[1] <= 0x7e {
				ch <- "alt-" + string(rune(pending[1]))
				pending = pending[2:]
				continue
			}
			// ESC followed by a control byte (0x00-0x1f, 0x7f) emits
			// Alt+key, matching the Alt+printable rule: Alt+Enter is
			// "alt-enter", Alt+Backspace is "alt-backspace", Alt+Tab
			// is "alt-tab", and Alt+Ctrl+A is "alt-ctrl-a". ESC
			// followed by ESC drops the first ESC: the second may
			// start a new sequence or be a lone ESC.
			if pending[1] == 0x1b {
				pending = pending[1:]
				continue
			}
			if key := singleKeyName(pending[1]); key != "" {
				ch <- "alt-" + key
			}
			pending = pending[2:]
			continue
		}
		// A multi-byte UTF-8 sequence: collect the full rune and emit
		// the decoded character. Terminals send text input as complete
		// UTF-8 sequences, so a byte >= 0x80 is the start of one.
		if pending[0] >= 0x80 {
			consumed, key := decodeUTF8Key(pending)
			if consumed == 0 {
				return pending // need more bytes
			}
			if key != "" {
				ch <- key
			}
			pending = pending[consumed:]
			continue
		}
		emitSingleKey(pending[0], ch)
		pending = pending[1:]
	}
	return pending
}

// consumeControlString finds the end of an OSC, DCS, SOS, PM, or APC
// control string and returns the number of bytes consumed, including
// the introducer and terminator. The terminator is BEL (0x07) or ST
// (ESC \). A return of -1 means the sequence is incomplete and more
// bytes are needed. See TheoryOfKeyInput.
func consumeControlString(pending []byte) int {
	for i := 2; i < len(pending); i++ {
		switch pending[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 >= len(pending) {
				return -1
			}
			if pending[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return -1
}

// decodeUTF8Key attempts to decode a multi-byte UTF-8 sequence at the
// start of pending. It returns the number of bytes consumed and the
// decoded character as a string. A return of (0, "") means the sequence
// is incomplete and more bytes are needed; a return of (1, "") means the
// leading byte is invalid and should be skipped.
func decodeUTF8Key(pending []byte) (consumed int, key string) {
	if len(pending) == 0 || pending[0] < 0x80 {
		return 0, ""
	}
	// Determine the expected length from the leading byte.
	var expected int
	switch {
	case pending[0] >= 0xF0:
		expected = 4
	case pending[0] >= 0xE0:
		expected = 3
	case pending[0] >= 0xC0:
		expected = 2
	default:
		// A continuation byte (0x80-0xBF) without a start byte is
		// invalid: skip it.
		return 1, ""
	}
	if len(pending) < expected {
		// Need more bytes to complete the sequence.
		return 0, ""
	}
	r, size := utf8.DecodeRune(pending[:expected])
	if r == utf8.RuneError {
		// Invalid UTF-8 sequence: skip the leading byte.
		return 1, ""
	}
	return size, string(r)
}

// ss3KeyName maps an SS3 final byte to its logical key name, or "" for
// an unrecognized final byte. It is shared by the simple and modified
// SS3 paths so both produce identical key names for the same final byte.
func ss3KeyName(final string) string {
	if len(final) == 0 {
		return ""
	}
	switch final[0] {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'E':
		return "begin"
	case 'H':
		return "home"
	case 'F':
		return "end"
	case 'P':
		return "f1"
	case 'Q':
		return "f2"
	case 'R':
		return "f3"
	case 'S':
		return "f4"
	case 'M':
		return "kp-enter"
	case 'X':
		return "kp-equal"
	case 'j':
		return "kp-multiply"
	case 'k':
		return "kp-add"
	case 'l':
		return "kp-comma"
	case 'm':
		return "kp-subtract"
	case 'n':
		return "kp-decimal"
	case 'o':
		return "kp-divide"
	case 'p':
		return "kp-0"
	case 'q':
		return "kp-1"
	case 'r':
		return "kp-2"
	case 's':
		return "kp-3"
	case 't':
		return "kp-4"
	case 'u':
		return "kp-5"
	case 'v':
		return "kp-6"
	case 'w':
		return "kp-7"
	case 'x':
		return "kp-8"
	case 'y':
		return "kp-9"
	}
	return ""
}

func emitSS3Key(b byte, ch chan<- string) {
	if key := ss3KeyName(string(b)); key != "" {
		ch <- key
	}
}

// emitSS3KeyWithMods handles modified SS3 sequences (ESC O <params>
// <final>). The last numeric parameter is the modifier bitfield, using
// the same 1 + Shift/Alt/Ctrl convention as CSI modified keys. The key
// name is derived from the final byte via ss3KeyName, so modified and
// unmodified SS3 keys produce consistent names.
func emitSS3KeyWithMods(params, final string, ch chan<- string) {
	if params == "" {
		if key := ss3KeyName(final); key != "" {
			ch <- key
		}
		return
	}
	parts := strings.Split(params, ";")
	mod, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || mod < 1 {
		return
	}
	key := ss3KeyName(final)
	if key == "" {
		return
	}
	if prefix := modifierPrefix(mod); prefix != "" {
		ch <- prefix + "-" + key
	} else {
		ch <- key
	}
}

func emitCSIKey(params, final string, ch chan<- string) {
	if final == "u" {
		emitKittyKey(params, ch)
		return
	}
	parts := strings.Split(params, ";")
	// xterm modifyOtherKeys format: CSI 27;<mod>;<code>~
	// The first parameter is always 27, identifying the format; the
	// second is the modifier bitfield; the third is the Unicode code
	// point of the key. The code is mapped through kittyKeyName so
	// both protocols produce consistent key names. Some terminals
	// append extra parameters; they are ignored.
	if final == "~" && len(parts) >= 3 && parts[0] == "27" {
		emitModifyOtherKeys(parts, ch)
		return
	}
	// URXVT mouse format (DECSET 1015): ESC [ Cb ; Cx ; Cy M, where Cb
	// is the button code offset by 32. No standard CSI key uses final
	// byte M with three numeric parameters, so this is unambiguous.
	if final == "M" && len(parts) == 3 {
		handleURXVTMouse(parts, ch)
		return
	}
	if len(parts) >= 2 {
		keyCode := parts[0]
		mod, err := strconv.Atoi(parts[1])
		if err != nil || mod < 1 {
			return
		}
		key := csiKeyCode(keyCode, final)
		if key == "" {
			return
		}
		if prefix := modifierPrefix(mod); prefix != "" {
			ch <- prefix + "-" + key
		} else {
			ch <- key
		}
		return
	}
	if key := csiSimpleKey(params, final); key != "" {
		ch <- key
	}
}

// emitModifyOtherKeys decodes the xterm modifyOtherKeys format
// (CSI 27;<mod>;<code> ~). The modifier follows the same 1 + bitfield
// convention as other CSI modified keys, and the code is a Unicode code
// point mapped through kittyKeyName for consistency with the Kitty
// protocol.
func emitModifyOtherKeys(parts []string, ch chan<- string) {
	mod, err := strconv.Atoi(parts[1])
	if err != nil || mod < 1 {
		return
	}
	code, err := strconv.Atoi(parts[2])
	if err != nil {
		return
	}
	key := kittyKeyName(code)
	if key == "" {
		return
	}
	if prefix := modifierPrefix(mod); prefix != "" {
		ch <- prefix + "-" + key
	} else {
		ch <- key
	}
}

func csiSimpleKey(params, final string) string {
	switch final {
	case "A":
		return "up"
	case "B":
		return "down"
	case "C":
		return "right"
	case "D":
		return "left"
	case "E":
		return "begin"
	case "H":
		return "home"
	case "F":
		return "end"
	case "I":
		return "focus-in"
	case "O":
		return "focus-out"
	case "Z":
		return "shift-tab"
	case "P":
		return "f1"
	case "Q":
		return "f2"
	case "R":
		return "f3"
	case "S":
		return "f4"
	case "~":
		switch params {
		case "1", "7":
			return "home"
		case "2":
			return "insert"
		case "3":
			return "delete"
		case "4", "8":
			return "end"
		case "5":
			return "pageup"
		case "6":
			return "pagedown"
		case "11":
			return "f1"
		case "12":
			return "f2"
		case "13":
			return "f3"
		case "14":
			return "f4"
		case "15":
			return "f5"
		case "17":
			return "f6"
		case "18":
			return "f7"
		case "19":
			return "f8"
		case "20":
			return "f9"
		case "21":
			return "f10"
		case "23":
			return "f11"
		case "24":
			return "f12"
		case "28":
			return "f13"
		case "29":
			return "f14"
		case "31":
			return "f15"
		case "32":
			return "f16"
		case "33":
			return "f17"
		case "34":
			return "f18"
		case "35":
			return "f19"
		case "36":
			return "f20"
		case "37":
			return "f21"
		case "38":
			return "f22"
		case "39":
			return "f23"
		case "40":
			return "f24"
		case "41":
			return "f25"
		case "42":
			return "f26"
		case "43":
			return "f27"
		case "44":
			return "f28"
		case "45":
			return "f29"
		case "46":
			return "f30"
		case "47":
			return "f31"
		case "48":
			return "f32"
		case "49":
			return "f33"
		case "50":
			return "f34"
		case "51":
			return "f35"
		case "52":
			return "f36"
		case "200":
			return "paste-start"
		case "201":
			return "paste-end"
		}
	}
	return ""
}

func csiKeyCode(keyCode, final string) string {
	switch final {
	case "A", "B", "C", "D", "E", "H", "F", "I", "O", "P", "Q", "S":
		return csiSimpleKey("", final)
	case "~":
		return csiSimpleKey(keyCode, final)
	}
	return ""
}

// linuxConsoleKey maps a Linux console function key final byte to its
// logical key name. The Linux console sends F1-F5 as ESC [ [ A-E and
// Shift+F1-F5 as ESC [ [ a-e, using a non-standard second [ that is
// recognized as a specific pattern before the generic CSI scan.
func linuxConsoleKey(b byte) string {
	switch b {
	case 'A':
		return "f1"
	case 'B':
		return "f2"
	case 'C':
		return "f3"
	case 'D':
		return "f4"
	case 'E':
		return "f5"
	case 'a':
		return "shift-f1"
	case 'b':
		return "shift-f2"
	case 'c':
		return "shift-f3"
	case 'd':
		return "shift-f4"
	case 'e':
		return "shift-f5"
	}
	return ""
}

// modifierPrefix converts an xterm SGR modifier parameter to a
// dash-joined prefix. The parameter is 1 + bitfield (1=Shift, 2=Alt,
// 4=Ctrl, 8=Meta, 16=Super, 32=Hyper), so parameter 2 is "shift", 5 is
// "ctrl", 9 is "meta", 17 is "super", and 33 is "hyper".
func modifierPrefix(mod int) string {
	mod--
	var parts []string
	if mod&1 != 0 {
		parts = append(parts, "shift")
	}
	if mod&2 != 0 {
		parts = append(parts, "alt")
	}
	if mod&4 != 0 {
		parts = append(parts, "ctrl")
	}
	if mod&8 != 0 {
		parts = append(parts, "meta")
	}
	if mod&16 != 0 {
		parts = append(parts, "super")
	}
	if mod&32 != 0 {
		parts = append(parts, "hyper")
	}
	return strings.Join(parts, "-")
}

// kittyModifierPrefix converts a Kitty keyboard protocol modifier
// parameter to a dash-joined prefix. The Kitty protocol uses a
// different bit assignment from xterm's CSI modified keys: bit 3 is
// Super, bit 4 is Hyper, bit 5 is Meta. Caps Lock (bit 6) and Num Lock
// (bit 7) are lock states, not transient modifiers, so they are not
// included in the prefix. The parameter is 1 + bitfield, so parameter
// 2 is "shift", 5 is "ctrl", 9 is "super", 17 is "hyper", and 33 is
// "meta".
func kittyModifierPrefix(mod int) string {
	mod--
	var parts []string
	if mod&1 != 0 {
		parts = append(parts, "shift")
	}
	if mod&2 != 0 {
		parts = append(parts, "alt")
	}
	if mod&4 != 0 {
		parts = append(parts, "ctrl")
	}
	if mod&8 != 0 {
		parts = append(parts, "super")
	}
	if mod&16 != 0 {
		parts = append(parts, "hyper")
	}
	if mod&32 != 0 {
		parts = append(parts, "meta")
	}
	return strings.Join(parts, "-")
}

func emitSingleKey(b byte, ch chan<- string) {
	if key := singleKeyName(b); key != "" {
		ch <- key
	}
}

func singleKeyName(b byte) string {
	switch b {
	case 0x00:
		return "ctrl-space"
	case 0x7f, 0x08:
		return "backspace"
	case 0x0d, 0x0a:
		return "enter"
	case 0x20:
		return "space"
	case '\t':
		return "tab"
	default:
		switch {
		case b >= 0x01 && b <= 0x1a:
			return "ctrl-" + string(rune('a'+b-1))
		case b >= 0x1c && b <= 0x1f:
			return "ctrl-" + string(rune(b|0x40))
		case b >= 0x20 && b <= 0x7e:
			return string(rune(b))
		}
		return ""
	}
}

func emitKittyKey(params string, ch chan<- string) {
	// Mode-setting and query-response sequences (CSI > flags u,
	// CSI < flags u, CSI ? flags u) are consumed silently: they
	// are Kitty keyboard protocol mode management, not key events.
	if len(params) > 0 {
		switch params[0] {
		case '<', '>', '?':
			return
		}
	}
	parts := strings.Split(params, ";")
	if len(parts) < 1 {
		return
	}
	code, err := strconv.Atoi(parts[0])
	if err != nil {
		return
	}
	mod := 1
	if len(parts) >= 2 {
		if mod, err = strconv.Atoi(parts[1]); err != nil {
			return
		}
	}
	event := 1
	if len(parts) >= 3 {
		if event, err = strconv.Atoi(parts[2]); err != nil {
			return
		}
	}
	// Release events (event 0) are ignored: the Kitty protocol reports
	// both press and release, and emitting both would double every key.
	if event == 0 {
		return
	}
	key := kittyKeyName(code)
	if key == "" {
		return
	}
	if mod > 1 {
		// The Kitty modifier encoding differs from xterm's: bit 3 is
		// Super, bit 4 is Hyper, bit 5 is Meta (xterm: Meta, Super,
		// Hyper). kittyModifierPrefix maps the Kitty encoding.
		if prefix := kittyModifierPrefix(mod); prefix != "" {
			ch <- prefix + "-" + key
		} else {
			ch <- key
		}
	} else {
		ch <- key
	}
}

func kittyKeyName(code int) string {
	switch code {
	case 9:
		return "tab"
	case 13:
		return "enter"
	case 27:
		return "esc"
	case 32:
		return "space"
	case 127:
		return "backspace"
	// Keypad keys (Kitty keyboard protocol code points in the
	// Private Use Area, per the Kitty keyboard protocol specification).
	case 57344:
		return "kp-enter"
	case 57345:
		return "kp-f1"
	case 57346:
		return "kp-f2"
	case 57347:
		return "kp-f3"
	case 57348:
		return "kp-f4"
	case 57349:
		return "kp-home"
	case 57350:
		return "kp-end"
	case 57351:
		return "kp-pageup"
	case 57352:
		return "kp-pagedown"
	case 57353:
		return "kp-left"
	case 57354:
		return "kp-right"
	case 57355:
		return "kp-up"
	case 57356:
		return "kp-down"
	case 57357:
		return "kp-begin"
	// Function keys F1-F24 (Kitty keyboard protocol code points in the
	// Private Use Area, per the Kitty keyboard protocol specification).
	case 57358:
		return "f1"
	case 57359:
		return "f2"
	case 57360:
		return "f3"
	case 57361:
		return "f4"
	case 57362:
		return "f5"
	case 57363:
		return "f6"
	case 57364:
		return "f7"
	case 57365:
		return "f8"
	case 57366:
		return "f9"
	case 57367:
		return "f10"
	case 57368:
		return "f11"
	case 57369:
		return "f12"
	case 57370:
		return "f13"
	case 57371:
		return "f14"
	case 57372:
		return "f15"
	case 57373:
		return "f16"
	case 57374:
		return "f17"
	case 57375:
		return "f18"
	case 57376:
		return "f19"
	case 57377:
		return "f20"
	case 57378:
		return "f21"
	case 57379:
		return "f22"
	case 57380:
		return "f23"
	case 57381:
		return "f24"
	// Navigation keys.
	case 57382:
		return "insert"
	case 57383:
		return "delete"
	case 57384:
		return "home"
	case 57385:
		return "end"
	case 57386:
		return "pageup"
	case 57387:
		return "pagedown"
	// Special keys.
	case 57388:
		return "print-screen"
	case 57389:
		return "scroll-lock"
	case 57390:
		return "pause"
	case 57391:
		return "menu"
	// Arrow keys.
	case 57394:
		return "left"
	case 57395:
		return "right"
	case 57396:
		return "up"
	case 57397:
		return "down"
	// Modifier-only key events (Kitty keyboard protocol code points
	// in the Private Use Area, per the Kitty keyboard protocol
	// specification). These are emitted when the user presses a
	// modifier key alone, not in combination with another key.
	case 57440:
		return "left-shift"
	case 57441:
		return "left-ctrl"
	case 57442:
		return "left-alt"
	case 57443:
		return "left-meta"
	case 57444:
		return "left-hyper"
	case 57445:
		return "right-shift"
	case 57446:
		return "right-ctrl"
	case 57447:
		return "right-alt"
	case 57448:
		return "right-meta"
	case 57449:
		return "right-hyper"
	case 57450:
		return "iso-level3-shift"
	case 57451:
		return "iso-level5-shift"
	case 57452:
		return "caps-lock"
	case 57453:
		return "num-lock"
	}
	// ASCII range: the same names as single-byte input, so the Kitty
	// and traditional paths produce consistent key names.
	if code >= 0 && code <= 0x7f {
		return singleKeyName(byte(code))
	}
	// Non-ASCII Unicode characters: return the character itself.
	if code > 0x7f && code <= 0x10FFFF {
		return string(rune(code))
	}
	return ""
}

// mouseExtendedButtonFlag is bit 7 of the mouse button code: it marks
// an extended button (buttons 8-11). The low two bits hold the button
// number offset (0-3 → 8-11).
const mouseExtendedButtonFlag = 128

func mouseKeyName(button, x, y int, release bool) string {
	// Keyboard modifiers (Shift, Alt, Ctrl) are encoded in bits 2-4 of
	// the button code, matching the xterm mouse protocol. They are
	// extracted and prepended as a dash-joined prefix to the event
	// name, so a Shift+click is "shift-mouse-left" and a Ctrl+wheel-up
	// is "ctrl-mouse-wheel-up", consistent with keyboard modifier names.
	var modifiers []string
	if button&mouseShiftFlag != 0 {
		modifiers = append(modifiers, "shift")
	}
	if button&mouseAltFlag != 0 {
		modifiers = append(modifiers, "alt")
	}
	if button&mouseCtrlFlag != 0 {
		modifiers = append(modifiers, "ctrl")
	}
	modPrefix := ""
	if len(modifiers) > 0 {
		modPrefix = strings.Join(modifiers, "-") + "-"
	}

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
		return fmt.Sprintf("%s%srelease@%d,%d", MouseKeyPrefix, modPrefix, x, y)
	case button&mouseExtendedButtonFlag != 0:
		// Extended buttons (8-11): bit 7 marks an extended button.
		// The low two bits hold the button number offset (0-3 → 8-11).
		// A motion event with the extended flag is a drag; otherwise
		// it is a press.
		btn := (button & 3) + 8
		if button&mouseMotionFlag != 0 {
			kind = fmt.Sprintf("button-%d-drag", btn)
		} else {
			kind = fmt.Sprintf("button-%d", btn)
		}
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
	if kind == "" {
		return ""
	}
	return fmt.Sprintf("%s%s%s@%d,%d", MouseKeyPrefix, modPrefix, kind, x, y)
}

func handleSGRMouse(pending []byte, ch chan<- string) int {
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
		if pending[i] == 'u' {
			// Not a mouse sequence: a Kitty keyboard mode pop
			// (CSI < flags u). Signal the caller to handle it as
			// a regular CSI sequence.
			return -2
		}
	}
	if end < 0 {
		return -1
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
			if key := mouseKeyName(button, x-1, y-1, release); key != "" {
				ch <- key
			}
		}
	}
	return end + 1
}

func handleX10Mouse(data []byte, ch chan<- string) {
	if len(data) < 3 {
		return
	}
	button := int(data[0]) - 32
	x := int(data[1]) - 32
	y := int(data[2]) - 32
	if button < 0 || x < 1 || y < 1 {
		return
	}
	// Release is indicated by the low 2 bits set to 3, not by the full
	// button value, so modifier bits (Shift, Alt, Ctrl) do not mask the
	// release detection.
	release := (button & 3) == 3
	if key := mouseKeyName(button, x-1, y-1, release); key != "" {
		ch <- key
	}
}

func handleURXVTMouse(parts []string, ch chan<- string) {
	if len(parts) != 3 {
		return
	}
	var cb, cx, cy int
	var parseErr error
	if cb, parseErr = strconv.Atoi(parts[0]); parseErr == nil {
		if cx, parseErr = strconv.Atoi(parts[1]); parseErr == nil {
			cy, parseErr = strconv.Atoi(parts[2])
		}
	}
	if parseErr != nil {
		return
	}
	button := cb - 32
	if button < 0 || cx < 1 || cy < 1 {
		return
	}
	// Release is indicated by the low 2 bits set to 3, not by the full
	// button value, so modifier bits (Shift, Alt, Ctrl) do not mask the
	// release detection.
	release := (button & 3) == 3
	if key := mouseKeyName(button, cx-1, cy-1, release); key != "" {
		ch <- key
	}
}
