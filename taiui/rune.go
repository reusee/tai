package taiui

import (
	"sync"

	textwidth "golang.org/x/text/width"
)

var runeWidths sync.Map

func RuneDisplayWidth(r rune) int {
	if v, ok := runeWidths.Load(r); ok {
		return v.(int)
	}
	prop := textwidth.LookupRune(r)
	kind := prop.Kind()
	w := 1
	if kind == textwidth.EastAsianAmbiguous ||
		kind == textwidth.EastAsianWide ||
		kind == textwidth.EastAsianFullwidth {
		w = 2
	}
	runeWidths.Store(r, w)
	return w
}

func RunesDisplayWidth(runes []rune) (l int) {
	for _, r := range runes {
		l += RuneDisplayWidth(r)
	}
	return
}
