package conversation_usecase

import (
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/conversation"
)

const sendRequestClaimTTL = 10 * time.Minute

type sendRequestLedger struct {
	messages conversation.MessageRepository
	claims   cache.SharedState
}

func NewSendRequestLedger(messages conversation.MessageRepository, claims cache.SharedState) conversation.SendRequestLedger {
	return &sendRequestLedger{messages: messages, claims: claims}
}

func (l *sendRequestLedger) Sent(request conversation.SendRequest) (*conversation.Message, error) {
	return l.messages.GetByClientRequestID(request.EntryType, request.EntryID, request.RequestID)
}

func (l *sendRequestLedger) Claim(request conversation.SendRequest) (bool, error) {
	return l.claims.SetNX(sendRequestClaimKey(request), "1", sendRequestClaimTTL)
}

func (l *sendRequestLedger) Release(request conversation.SendRequest) {
	if err := l.claims.Del(sendRequestClaimKey(request)); err != nil {
		log.Printf("[OperatorSend] could not release send request %s on %s %s: %v", request.RequestID, request.EntryType, request.EntryID, err)
	}
}

func sendRequestClaimKey(request conversation.SendRequest) string {
	return "operator_send_request:" + string(request.EntryType) + ":" + request.EntryID + ":" + request.RequestID
}
