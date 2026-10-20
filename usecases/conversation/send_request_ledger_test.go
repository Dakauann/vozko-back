package conversation_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type ledgerClaims struct {
	cache.SharedState
	held map[string]time.Duration
	err  error
}

func (c *ledgerClaims) SetNX(key, _ string, ttl time.Duration) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	if _, taken := c.held[key]; taken {
		return false, nil
	}
	c.held[key] = ttl
	return true, nil
}

func (c *ledgerClaims) Del(keys ...string) error {
	for _, key := range keys {
		delete(c.held, key)
	}
	return nil
}

type ledgerMessages struct {
	conversation.MessageRepository
	stored map[conversation.SendRequest]*conversation.Message
}

func (m ledgerMessages) GetByClientRequestID(entryType shared.EntryType, entryID, requestID string) (*conversation.Message, error) {
	if message, ok := m.stored[conversation.SendRequest{EntryType: entryType, EntryID: entryID, RequestID: requestID}]; ok {
		return message, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func TestTheLedgerFindsAStoredSendByItsRequest(t *testing.T) {
	request := conversation.SendRequest{EntryType: shared.EntryTypeWhatsApp, EntryID: "entry-1", RequestID: "req-1"}
	stored := &conversation.Message{ID: "msg-1"}
	ledger := NewSendRequestLedger(ledgerMessages{stored: map[conversation.SendRequest]*conversation.Message{request: stored}}, &ledgerClaims{held: map[string]time.Duration{}})

	if got, err := ledger.Sent(request); err != nil || got != stored {
		t.Fatalf("Sent = %v, %v; want the stored message", got, err)
	}
	other := request
	other.EntryID = "entry-2"
	if _, err := ledger.Sent(other); !errors.Is(err, conversation.ErrMessageNotFound) {
		t.Fatalf("another conversation's request matched, err = %v", err)
	}
}

func TestTheLedgerLetsOneSenderClaimARequestForTenMinutes(t *testing.T) {
	claims := &ledgerClaims{held: map[string]time.Duration{}}
	ledger := NewSendRequestLedger(ledgerMessages{}, claims)
	request := conversation.SendRequest{EntryType: shared.EntryTypeWhatsApp, EntryID: "entry-1", RequestID: "req-1"}

	if ok, err := ledger.Claim(request); !ok || err != nil {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	if ok, _ := ledger.Claim(request); ok {
		t.Fatal("a second replica claimed a request already being sent")
	}
	for _, ttl := range claims.held {
		if ttl != 10*time.Minute {
			t.Fatalf("claim lives %v, want 10 minutes", ttl)
		}
	}
	other := request
	other.EntryType = shared.EntryTypeTelegram
	if ok, _ := ledger.Claim(other); !ok {
		t.Fatal("the same request id on another channel is a different send")
	}

	ledger.Release(request)
	if ok, _ := ledger.Claim(request); !ok {
		t.Fatal("a released request could not be retried")
	}
}

func TestTheLedgerSurfacesAClaimStoreFailure(t *testing.T) {
	ledger := NewSendRequestLedger(ledgerMessages{}, &ledgerClaims{err: errors.New("redis down")})
	if _, err := ledger.Claim(conversation.SendRequest{EntryType: shared.EntryTypeWhatsApp, EntryID: "entry-1", RequestID: "req-1"}); err == nil {
		t.Fatal("a claim store failure must not look like a free claim")
	}
}
