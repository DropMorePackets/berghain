package berghain

import (
	"encoding/json"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

var validatorNoneChallenge noneChallengeTemplate

type noneChallengeTemplate struct {
	once sync.Once
	raw  string

	// Slot accessors; valid after init ran.
	Countdown bufSlot // 1 byte, '0'..'9'
	SupportID bufSlot // 39 support-ID chars, echoed for the challenge page
}

func (t *noneChallengeTemplate) init() {
	t.once.Do(func() {
		const (
			countdown = "0"
			echoID    = "bh@00000000-0000-4000-8000-000000000001"
		)
		t.raw = mustJSONEncodeString(struct {
			Countdown json.RawMessage `json:"c"`
			Type      int             `json:"t"`
			SupportID string          `json:"i"`
		}{
			Countdown: json.RawMessage(countdown),
			Type:      0,
			SupportID: echoID,
		})

		loc := slotLocator{doc: t.raw}
		t.Countdown = loc.next(countdown)
		t.SupportID = loc.next(echoID)
	})
}

// Render appends the template to body and returns the rendered document.
// Slot accessors take this document and return writable views into it.
func (t *noneChallengeTemplate) Render(body *buffer.SliceBuffer) []byte {
	t.init()
	return renderTemplate(body, t.raw)
}

func validatorNone(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	if !ValidSupportID(req.SupportID) {
		return ErrInvalidLength
	}

	lc := b.LevelConfig(req.Identifier.Level)

	tpl := &validatorNoneChallenge
	doc := tpl.Render(resp.Body)
	tpl.Countdown(doc)[0] = byte('0' + lc.Countdown)
	copy(tpl.SupportID(doc), req.SupportID)

	return req.Identifier.ToCookie(b, resp.Token)
}
