package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

type fakeGuide struct {
	options []mldomain.ReasonOption
	caps    []mldomain.ReasonCap
	sent    []mldomain.ReasonSend
	sendErr error
}

func (f *fakeGuide) Options(context.Context, mldomain.Credentials, string) ([]mldomain.ReasonOption, error) {
	return f.options, nil
}

func (f *fakeGuide) Caps(context.Context, mldomain.Credentials, string) ([]mldomain.ReasonCap, error) {
	return f.caps, nil
}

func (f *fakeGuide) SendOption(_ context.Context, _ mldomain.Credentials, _ string, send mldomain.ReasonSend) (*mldomain.RemoteMessage, error) {
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	f.sent = append(f.sent, send)
	return &mldomain.RemoteMessage{ID: "0123456789abcdefaaaa", Text: send.Text, Status: mldomain.MessageAvailable, CreatedAt: testNow}, nil
}

type fakeAccess struct{ allowed bool }

func (f fakeAccess) CanAccessEntry(string, string, string, string, bool) bool { return f.allowed }

type fakeRefresher struct{ refreshed []string }

func (f *fakeRefresher) RefreshState(_ context.Context, _ *mldomain.Account, conv *mldomain.Conversation) error {
	f.refreshed = append(f.refreshed, conv.ID)
	return nil
}

type reasonsFixture struct {
	reasons       *Reasons
	guide         *fakeGuide
	history       *fakeHistory
	conversations *fakeConversations
	refresher     *fakeRefresher
	metrics       *fakeMetrics
	accounts      *fakeAccounts
}

func newReasonsFixture(allowed bool) *reasonsFixture {
	f := &reasonsFixture{
		guide: &fakeGuide{
			options: []mldomain.ReasonOption{{ID: mldomain.ReasonOther, Enabled: true, Actionable: true, CharLimit: 350, CapAvailable: 1}},
			caps:    []mldomain.ReasonCap{{OptionID: mldomain.ReasonOther, CapAvailable: 1}},
		},
		history:       &fakeHistory{},
		conversations: newFakeConversations(),
		refresher:     &fakeRefresher{},
		metrics:       newFakeMetrics(),
		accounts:      newFakeAccounts(),
	}
	f.accounts.put(connectedAccount())
	f.conversations.put(&mldomain.Conversation{ID: "conv-1", WorkspaceID: "ws-1", AccountID: "acct-1", ContactID: "ct-1", PackKey: packKey})
	tokens := NewTokenSource(f.accounts, &fakeOAuth{refresh: rotated}, fakeClassifier{}, &fakeAlerts{}, f.metrics)
	tokens.now = fixedClock
	f.reasons = NewReasons(ReasonsDeps{
		Accounts: f.accounts, Conversations: f.conversations, Services: Services{ActionGuide: f.guide, Errors: fakeClassifier{}},
		Tokens: tokens, Threads: f.refresher, History: f.history, Access: fakeAccess{allowed: allowed}, Metrics: f.metrics,
	})
	f.reasons.now = fixedClock
	return f
}

func operatorQuery() ConversationQuery {
	return ConversationQuery{Actor: conversation.SentByPerson("user-1"), WorkspaceID: "ws-1", EntryID: "conv-1"}
}

func TestReasonsListTheOptionsWithTheirLiveCaps(t *testing.T) {
	f := newReasonsFixture(true)
	f.guide.caps = []mldomain.ReasonCap{{OptionID: mldomain.ReasonOther, CapAvailable: 0}}
	options, err := f.reasons.Options(context.Background(), operatorQuery())
	if err != nil || len(options) != 1 || options[0].CapAvailable != 0 {
		t.Fatalf("options = %+v %v", options, err)
	}
}

func TestAnOperatorStartsAConversationThroughAReason(t *testing.T) {
	f := newReasonsFixture(true)
	out, err := f.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: operatorQuery(), OptionID: mldomain.ReasonOther, Text: "Olá! Seu pedido sai amanhã."})
	if err != nil {
		t.Fatal(err)
	}
	if out.ProviderMessageID != "0123456789abcdefaaaa" || len(f.guide.sent) != 1 {
		t.Fatalf("out = %+v sent = %v", out, f.guide.sent)
	}
	if len(f.history.records) != 1 {
		t.Fatalf("history = %v", f.history.records)
	}
	rec := f.history.records[0]
	if rec.SentBy.Kind() != conversation.SenderHuman || rec.SentBy.ID() != "user-1" || rec.EntryType != shared.EntryTypeMercadoLivre ||
		rec.MessageType != conversation.MessageTypeOperator || rec.ProviderMessageID != out.ProviderMessageID || rec.Text != "Olá! Seu pedido sai amanhã." {
		t.Fatalf("record = %+v", rec)
	}
	var meta map[string]any
	if err := json.Unmarshal(rec.Metadata, &meta); err != nil || meta[reasonMetadataKey] != mldomain.ReasonOther {
		t.Fatalf("metadata = %s", rec.Metadata)
	}
	if f.conversations.outbound != 1 || joined(f.refresher.refreshed) != "conv-1" || f.metrics.count("reasons/sent") != 1 {
		t.Fatalf("outbound=%d refreshed=%v metrics=%v", f.conversations.outbound, f.refresher.refreshed, f.metrics.events)
	}
}

func TestOnlyAPersonWhoSeesTheConversationMayUseAReason(t *testing.T) {
	for name, actor := range map[string]conversation.SentBy{
		"agent":    conversation.SentByAI("agent-1"),
		"workflow": conversation.SentByWorkflow("wf-1"),
		"system":   conversation.SentBySystem(),
		"nobody":   conversation.SentByPerson(""),
	} {
		f := newReasonsFixture(true)
		q := operatorQuery()
		q.Actor = actor
		if _, err := f.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: q, OptionID: mldomain.ReasonOther, Text: "oi"}); !errors.Is(err, mldomain.ErrPersonOnly) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, err := f.reasons.Options(context.Background(), q); !errors.Is(err, mldomain.ErrPersonOnly) {
			t.Errorf("%s options: err = %v", name, err)
		}
		if len(f.guide.sent) != 0 {
			t.Errorf("%s reached Mercado Livre", name)
		}
	}

	hidden := newReasonsFixture(false)
	if _, err := hidden.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: operatorQuery(), OptionID: mldomain.ReasonOther, Text: "oi"}); !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("a conversation the person cannot see = %v", err)
	}
	other := newReasonsFixture(true)
	q := operatorQuery()
	q.WorkspaceID = "ws-2"
	if _, err := other.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: q, OptionID: mldomain.ReasonOther, Text: "oi"}); !errors.Is(err, mldomain.ErrConversationNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
}

func TestAReasonIsCheckedBeforeItIsSent(t *testing.T) {
	f := newReasonsFixture(true)
	f.guide.caps = []mldomain.ReasonCap{{OptionID: mldomain.ReasonOther, CapAvailable: 0}}
	if _, err := f.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: operatorQuery(), OptionID: mldomain.ReasonOther, Text: "oi"}); !errors.Is(err, mldomain.ErrReasonCapReached) {
		t.Fatalf("spent cap = %v", err)
	}
	if _, err := f.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: operatorQuery(), OptionID: "SURPRISE", Text: "oi"}); !errors.Is(err, mldomain.ErrReasonUnknown) {
		t.Fatalf("unknown = %v", err)
	}
	acc := f.accounts.get("acct-1")
	acc.CanSendMessages = false
	f.accounts.put(acc)
	if _, err := f.reasons.Send(context.Background(), SendReasonInput{ConversationQuery: operatorQuery(), OptionID: mldomain.ReasonOther, Text: "oi"}); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("an account that cannot send = %v", err)
	}
	if len(f.guide.sent) != 0 || len(f.history.records) != 0 {
		t.Fatal("a refused reason reached Mercado Livre or the transcript")
	}
}

func TestTheDescriptorStillCannotStartConversations(t *testing.T) {
	if mldomain.Descriptor().Capabilities.CanInitiateConversation {
		t.Fatal("reasons are a manual operator action; the channel itself never starts conversations")
	}
}

func TestReasonTemplatesCarryTheirTextInTheAccountSite(t *testing.T) {
	f := newReasonsFixture(true)
	f.guide.options = []mldomain.ReasonOption{{
		ID: mldomain.ReasonDeliveryPromise, Description: "Promessa", Enabled: true, Actionable: true, CapAvailable: 1,
		Templates: []mldomain.ReasonTemplate{{ID: "TEMPLATE___DELIVERY_PROMISE___1", TextsBySite: map[string]string{"MLA": "Hola", "MLB": "Olá, entregaremos entre %d e %d h."}}},
	}}
	options, err := f.reasons.Options(context.Background(), operatorQuery())
	if err != nil || len(options) != 1 {
		t.Fatalf("options = %+v %v", options, err)
	}
	if tpl := options[0].Templates[0]; tpl.Text != "Olá, entregaremos entre %d e %d h." || tpl.TextsBySite != nil || options[0].Description != "Promessa" {
		t.Fatalf("option = %+v", options[0])
	}
}
