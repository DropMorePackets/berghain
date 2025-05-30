package berghain

import (
	"fmt"
)

const validatorNoneResponse = `{"c": 0, "t": 0}`

func validatorNone(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	lc := b.LevelConfig(req.Identifier.Level)

	copy(resp.Body.WriteBytes(), validatorNoneResponse)
	resp.Body.AdvanceW(6)
	copy(resp.Body.WriteNBytes(1), fmt.Sprintf("%d", lc.Countdown))
	resp.Body.AdvanceW(9)

	return req.Identifier.ToCookie(b, resp.Token)
}
