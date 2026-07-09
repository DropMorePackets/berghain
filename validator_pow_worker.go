package berghain

import "net/http"

var validatorPOWWorkerChallengeTemplate = powChallengeTemplate(2)

// validatorPOWWorker is a proof-of-work challenge identical to validatorPOW on
// the wire except for its type value (2), which tells the client to solve it
// inside a Web Worker. The server cannot prove a worker was actually used (the
// solution is byte-identical to a main-thread solve), so this exists to require
// the Web Worker capability on the client and verifies exactly like validatorPOW.
func validatorPOWWorker(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	p := powValidator{template: validatorPOWWorkerChallengeTemplate}

	switch req.Method {
	case http.MethodPost:
		if err := p.isValid(b, req, resp); err != nil {
			return err
		}
		return req.Identifier.ToCookie(b, resp.Token)
	case http.MethodGet:
		return p.onNew(b, req, resp)
	}

	return errInvalidMethod
}
