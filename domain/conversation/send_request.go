package conversation

import (
	"errors"
	"strings"

	"vozko/domain/shared"
)

const MaxClientRequestIDLength = 128

var (
	ErrClientRequestIDInvalid  = errors.New("conversation: the client request id is too long")
	ErrClientRequestIDRequired = errors.New("conversation: this command needs a client request id")
	ErrSendRequestInFlight     = errors.New("conversation: this send request is already being sent")
)

type SendRequest struct {
	EntryType shared.EntryType
	EntryID   string
	RequestID string
}

func NewSendRequest(entryType, entryID, requestID string) (SendRequest, bool, error) {
	request := SendRequest{EntryType: shared.EntryType(entryType), EntryID: strings.TrimSpace(entryID), RequestID: strings.TrimSpace(requestID)}
	if request.RequestID == "" {
		return SendRequest{}, false, nil
	}
	if len(request.RequestID) > MaxClientRequestIDLength {
		return SendRequest{}, false, ErrClientRequestIDInvalid
	}
	return request, true, nil
}

type SendRequestLedger interface {
	Sent(request SendRequest) (*Message, error)
	Claim(request SendRequest) (bool, error)
	Release(request SendRequest)
}
