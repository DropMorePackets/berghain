package berghain

import (
	"fmt"
	"strings"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

// jsonSlot returns the writable region of one fixed-width placeholder inside
// a rendered template document. Slots are created once at template init; the
// returned closure captures only the offset and length, so calls after init
// perform no allocations.
type jsonSlot func(doc []byte) []byte

// slotLocator finds placeholders sequentially in a marshaled template, so a
// placeholder only has to be unique in the region after the previous slot.
// Placeholders must be fixed-width and survive JSON encoding verbatim
// (hex digits, "bh@", and dashes all qualify - nothing that gets escaped).
type slotLocator struct {
	doc    string
	cursor int
}

func (l *slotLocator) next(placeholder string) jsonSlot {
	rel := strings.Index(l.doc[l.cursor:], placeholder)
	if rel < 0 {
		panic(fmt.Sprintf("jsontemplate: placeholder %q not found after offset %d in %q",
			placeholder, l.cursor, l.doc))
	}
	off := l.cursor + rel
	n := len(placeholder)
	l.cursor = off + n

	return func(doc []byte) []byte { return doc[off : off+n] }
}

// renderTemplate appends raw to body and returns the rendered document that
// the template's slot accessors write into.
func renderTemplate(body *buffer.SliceBuffer, raw string) []byte {
	doc := body.WriteNBytes(len(raw))
	copy(doc, raw)
	return doc
}
