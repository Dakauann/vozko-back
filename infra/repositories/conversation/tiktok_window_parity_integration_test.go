package conversation_repository_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
	tiktok_repository "vozko/infra/repositories/tiktok"
	ttuc "vozko/usecases/tiktok"
)

type ttOAuth struct{ ttdomain.OAuthService }
type ttMessaging struct{ ttdomain.MessagingService }
type ttErrors struct{ ttdomain.ErrorClassifier }
type ttAlerts struct{ ttuc.AccountAlerts }
type ttMetrics struct{ ttuc.Metrics }
type ttFetcher struct{ ttuc.MediaFetcher }
type ttSideState struct{}

func (ttSideState) SetString(key, value string, ttl time.Duration) error { return nil }
func (ttSideState) Exists(key string) (bool, error)                      { return false, nil }

func TestWindowFilterAgreesWithTheTikTokAdapter(t *testing.T) {
	w := newParityWorld(t, shared.EntryTypeTikTok, &schema.TikTokAccount{}, &schema.TikTokContact{}, &schema.TikTokConversation{})
	account := func(status string, canSend bool) *schema.TikTokAccount {
		record := &schema.TikTokAccount{WorkspaceID: w.workspace, OpenID: "_000" + uuid.NewString()[:12], Status: status,
			AccessToken: piigorm.NewEncrypted("token"), CanReceiveMessages: true, CanSendMessages: canSend}
		w.create(record)
		return record
	}
	ok := account("CONNECTED", true)
	conv := func(name string, owner *schema.TikTokAccount, threadID string, lastCustomer, budgetClosed *time.Time, spent int, open bool) {
		contact := &schema.TikTokContact{WorkspaceID: w.workspace, AccountID: owner.ID, UniqueIdentifier: "+" + uuid.NewString()}
		w.create(contact)
		c := &schema.TikTokConversation{WorkspaceID: w.workspace, AccountID: owner.ID, ContactID: contact.ID, ConversationID: threadID,
			OutboundSinceInbound: spent, BudgetClosedAt: budgetClosed, LastMessageAt: ago(time.Minute), LastCustomerMessageAt: lastCustomer}
		w.create(c)
		w.expect(name, c.ID, c.AccountID, open)
	}
	thread := func() string { return "S:1:0:" + uuid.NewString() }
	conv("open", ok, thread(), ago(time.Hour), nil, 3, true)
	conv("budget left", ok, thread(), ago(time.Hour), nil, ttdomain.OutboundBudget-1, true)
	conv("budget spent", ok, thread(), ago(time.Hour), nil, ttdomain.OutboundBudget, false)
	conv("window expired", ok, thread(), ago(ttdomain.MessagingWindow+time.Minute), nil, 0, false)
	conv("never wrote", ok, thread(), nil, nil, 0, false)
	conv("tiktok refused after the last message", ok, thread(), ago(time.Hour), ago(time.Minute), 1, false)
	conv("customer wrote after the refusal", ok, thread(), ago(time.Minute), ago(time.Hour), 0, true)
	conv("thread unknown", ok, "", ago(time.Hour), nil, 0, false)
	conv("account needs reconnect", account("NEEDS_RECONNECT", true), thread(), ago(time.Hour), nil, 0, false)
	conv("send permission lost", account("CONNECTED", false), thread(), ago(time.Hour), nil, 0, false)

	accounts := tiktok_repository.NewAccountRepository(w.db)
	adapter := ttuc.NewChannelAdapter(ttuc.ChannelAdapterDeps{
		Accounts: accounts, Contacts: tiktok_repository.NewContactRepository(w.db),
		Conversations: tiktok_repository.NewConversationRepository(w.db),
		Tokens:        ttuc.NewTokenSource(accounts, ttOAuth{}, ttErrors{}, ttAlerts{}, ttMetrics{}),
		Messaging:     ttMessaging{}, Errors: ttErrors{}, Fetcher: ttFetcher{},
		Side: ttuc.NewSideMessages(ttSideState{}), Metrics: ttMetrics{},
	})
	w.assertParity(adapter)
}
