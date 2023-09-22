package berghain

const validatorNoneResponse = `{"t": 0}`

func validatorNone(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	copy(resp.Body.WriteNBytes(len(validatorNoneResponse)), validatorNoneResponse)

	return req.Identifier.ToCookie(b, resp.Token)
}
