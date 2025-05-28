package berghain

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dropmorepackets/haproxy-go/pkg/buffer"
)

type ValidationType int

const (
	_ ValidationType = iota
	ValidationTypeNone
	ValidationTypePOW
)

type ValidatorResponse struct {
	Body  *buffer.SliceBuffer
	Token *buffer.SliceBuffer
}

var validatorResponsePool = sync.Pool{
	New: func() any {
		return &ValidatorResponse{
			Body:  buffer.NewSliceBuffer(1024), //TODO: use const for size
			Token: AcquireCookieBuffer(),
		}
	},
}

func AcquireValidatorResponse() *ValidatorResponse {
	return validatorResponsePool.Get().(*ValidatorResponse)
}

func ReleaseValidatorResponse(v *ValidatorResponse) {
	v.Token.Reset()
	v.Body.Reset()
	validatorResponsePool.Put(v)
}

type ValidatorRequest struct {
	Method     string
	Body       []byte
	Identifier *RequestIdentifier
}

var validatorRequestPool = sync.Pool{
	New: func() any {
		return &ValidatorRequest{}
	},
}

func AcquireValidatorRequest() *ValidatorRequest {
	return validatorRequestPool.Get().(*ValidatorRequest)
}

func ReleaseValidatorRequest(v *ValidatorRequest) {
	v.Method = ""
	v.Body = nil
	v.Identifier = nil
	validatorRequestPool.Put(v)
}

func (v ValidationType) RunValidator(b *Berghain, req *ValidatorRequest, resp *ValidatorResponse) error {
	switch v {
	case ValidationTypeNone:
		return validatorNone(b, req, resp)
	case ValidationTypePOW:
		return validatorPOW(b, req, resp)
	default:
		return errors.New("unknown validation type")
	}
}

var errInvalidMethod = fmt.Errorf("invalid method")
