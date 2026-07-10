package berghain

var validatorPOWWorkerChallengeTemplate = powChallengeTemplate(2)

// validatorPOWWorker asks the browser to solve the same POW inside a Web
// Worker. The proof is intentionally identical, so the server validates both
// variants through the same implementation and does not claim worker
// attestation.
func validatorPOWWorker(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	return powValidator{template: validatorPOWWorkerChallengeTemplate}.run(b, req, resp)
}
