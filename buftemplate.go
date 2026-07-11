package berghain

import (
	"fmt"
	"strings"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

// bufSlot returns the writable region of one fixed-width placeholder inside
// a rendered template document. Slots are created once at template init; the
// returned closure captures only the offset and length, so calls after init
// perform no allocations.
type bufSlot func(doc []byte) []byte

// slotLocator finds placeholders sequentially in a template document, so a
// placeholder only has to be unique in the region after the previous slot.
// Placeholders must be fixed-width and appear verbatim in the final document;
// for JSON templates that means nothing that gets escaped (hex digits, "bh@",
// and dashes all qualify).
type slotLocator struct {
	doc    string
	cursor int
}

func (l *slotLocator) next(placeholder string) bufSlot {
	rel := strings.Index(l.doc[l.cursor:], placeholder)
	if rel < 0 {
		panic(fmt.Sprintf("buftemplate: placeholder %q not found after offset %d in %q",
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
