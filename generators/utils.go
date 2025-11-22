package generators

import (
	"github.com/gabriel-vasile/mimetype"
)

var (
	K = 1 << 10
	M = 1 << 20
	G = 1 << 30
)

func isTextMIMEType(t string) bool {
	mtype := mimetype.Lookup(t)
	if mtype == nil {
		return false // Unknown MIME type, treat as not text
	}
	for t := mtype; t != nil; t = t.Parent() {
		if t.Is("text/plain") {
			return true
		}
	}
	return false
}
