package berghain

var validatorNoneResponse = mustJSONEncodeString(struct {
	Countdown int `json:"c"`
	Type      int `json:"t"`
}{
	// Only strings have to be set, as the default is zero for ints.
	// We do set the Type here because it is static anyway...
	Type: 0,
})

func validatorNone(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	lc := b.LevelConfig(req.Identifier.Level)

	copy(resp.Body.WriteBytes(), validatorNoneResponse)
	resp.Body.AdvanceW(len(`{"c":`))
	// the following conversion is faster than sprintf but also way uglier, I am sorry.
	// 48 is the ASCII code for '0', adding lc.Countdown will give us the single correct digit.
	copy(resp.Body.WriteNBytes(1), []byte{byte(48 + lc.Countdown)})
	resp.Body.AdvanceW(len(`,"t":0}`))

	return req.Identifier.ToCookie(b, resp.Token)
}
