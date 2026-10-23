package emailconsent_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailconsent"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
)

var now = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

type memorySubscriptions struct {
	rows  map[string]emailconsent.Subscription
	saves int
}

func newMemorySubscriptions() *memorySubscriptions {
	return &memorySubscriptions{rows: map[string]emailconsent.Subscription{}}
}

func key(ws string, a emailaddress.Address) string { return ws + "|" + string(a) }

func (m *memorySubscriptions) Get(_ context.Context, ws string, a emailaddress.Address) (*emailconsent.Subscription, error) {
	s, ok := m.rows[key(ws, a)]
	if !ok {
		return nil, emailconsent.ErrSubscriptionNotFound
	}
	return &s, nil
}

func (m *memorySubscriptions) Find(_ context.Context, ws string, as []emailaddress.Address) (map[emailaddress.Address]emailconsent.Subscription, error) {
	out := map[emailaddress.Address]emailconsent.Subscription{}
	for _, a := range as {
		if s, ok := m.rows[key(ws, a)]; ok {
			out[a] = s
		}
	}
	return out, nil
}

func (m *memorySubscriptions) Save(_ context.Context, s emailconsent.Subscription) (emailconsent.Subscription, error) {
	m.saves++
	m.rows[key(s.WorkspaceID, s.Address)] = s
	return s, nil
}

func (m *memorySubscriptions) SaveMany(ctx context.Context, subs []emailconsent.Subscription) error {
	for _, s := range subs {
		_, _ = m.Save(ctx, s)
	}
	return nil
}

type memoryMessages struct {
	rows    map[string]*emailsending.Message
	updates int
}

func (m *memoryMessages) Get(_ context.Context, id string) (*emailsending.Message, error) {
	msg, ok := m.rows[id]
	if !ok {
		return nil, emailsending.ErrMessageNotFound
	}
	copied := *msg
	return &copied, nil
}

func (m *memoryMessages) Update(_ context.Context, msg *emailsending.Message) error {
	m.updates++
	copied := *msg
	m.rows[msg.ID] = &copied
	return nil
}

type stubLeads map[string]string

func (s stubLeads) FindByEmail(_ string, email string) (*lead.Lead, error) {
	if id, ok := s[email]; ok {
		return &lead.Lead{ID: id}, nil
	}
	return nil, lead.ErrLeadNotFound
}

const messageID = "8f14e45f-ceea-467a-9a36-1e3ad4a1c2d0"

func fixture(t *testing.T) (*Service, *memorySubscriptions, *memoryMessages, *emailtoken.Codec) {
	t.Helper()
	codec, err := emailtoken.NewCodec("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	subs := newMemorySubscriptions()
	msgs := &memoryMessages{rows: map[string]*emailsending.Message{
		messageID: {ID: messageID, WorkspaceID: "ws", Address: "ana@example.com"},
	}}
	svc, err := New(Deps{Subscriptions: subs, Messages: msgs, Tokens: codec, Leads: stubLeads{"ana@example.com": "lead-ana"}, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return svc, subs, msgs, codec
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestSubscribeLinksTheLeadAndIsIdempotent(t *testing.T) {
	svc, subs, _, _ := fixture(t)
	first, err := svc.Subscribe(context.Background(), "ws", SubscribeInput{Address: "Ana@Example.com", Source: emailconsent.SourceForm, IP: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	if first.LeadID != "lead-ana" || first.Status != emailconsent.StatusSubscribed || !first.ConsentedAt.Equal(now) {
		t.Fatalf("first = %+v", first)
	}
	if _, err := svc.Subscribe(context.Background(), "ws", SubscribeInput{Address: "ana@example.com", Source: emailconsent.SourceOperator}); err != nil {
		t.Fatal(err)
	}
	if subs.saves != 1 {
		t.Fatalf("saves = %d, a repeated consent changes nothing", subs.saves)
	}
	if _, err := svc.Subscribe(context.Background(), "ws", SubscribeInput{Address: "ana@example.com", Source: emailconsent.SourceForm}); !errors.Is(err, emailconsent.ErrConsentIPRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportConsent(t *testing.T) {
	svc, subs, _, _ := fixture(t)
	gone, _ := emailconsent.NewUnsubscribed("ws", "gone@example.com", "", emailconsent.UnsubscribeLink, now)
	subs.rows[key("ws", "gone@example.com")] = gone
	kept, _ := emailconsent.NewSubscription(emailconsent.Consent{WorkspaceID: "ws", Address: "kept@example.com", Source: emailconsent.SourceForm, IP: "203.0.113.1", At: now}, now)
	subs.rows[key("ws", "kept@example.com")] = kept
	result, err := svc.ImportConsent(context.Background(), "ws", []ImportRow{
		{Address: "new@example.com", ConsentedAt: now.Add(-time.Hour)},
		{Address: "NEW@example.com", ConsentedAt: now.Add(-time.Hour)},
		{Address: "gone@example.com", ConsentedAt: now.Add(-time.Hour)},
		{Address: "kept@example.com", ConsentedAt: now.Add(-time.Hour)},
		{Address: "broken", ConsentedAt: now},
		{Address: "nodate@example.com"},
		{Address: "badip@example.com", IP: "x", ConsentedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Subscribed != 1 || result.Duplicates != 1 || result.KeptUnsubscribed != 1 || result.Unchanged != 1 || len(result.Invalid) != 3 {
		t.Fatalf("result = %+v", result)
	}
	want := []RowError{{Row: 5, Code: "address_invalid"}, {Row: 6, Code: "consent_date_required"}, {Row: 7, Code: "consent_ip_invalid"}}
	for i := range want {
		if result.Invalid[i] != want[i] {
			t.Fatalf("invalid = %+v", result.Invalid)
		}
	}
	if subs.rows[key("ws", "gone@example.com")].Status != emailconsent.StatusUnsubscribed {
		t.Fatal("an import never revives an opt-out")
	}
	if subs.rows[key("ws", "new@example.com")].Source != emailconsent.SourceImport {
		t.Fatal("the new address is stored as imported")
	}
}

func TestImportRefusesEmptyAndOversizedFiles(t *testing.T) {
	svc, _, _, _ := fixture(t)
	if _, err := svc.ImportConsent(context.Background(), "ws", nil); !errors.Is(err, emailconsent.ErrImportEmpty) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.ImportConsent(context.Background(), "ws", make([]ImportRow, emailconsent.MaxImportRows+1)); !errors.Is(err, emailconsent.ErrImportTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnsubscribeByToken(t *testing.T) {
	svc, subs, msgs, codec := fixture(t)
	token, _ := codec.Seal(emailtoken.PurposeUnsubscribe, messageID)
	sub, _ := emailconsent.NewSubscription(emailconsent.Consent{WorkspaceID: "ws", Address: "ana@example.com", Source: emailconsent.SourceImport, At: now}, now)
	subs.rows[key("ws", "ana@example.com")] = sub

	result, err := svc.Unsubscribe(context.Background(), token, emailconsent.UnsubscribeOneClick)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkspaceID != "ws" || result.Address != "ana@example.com" {
		t.Fatalf("result = %+v", result)
	}
	stored := subs.rows[key("ws", "ana@example.com")]
	if stored.Status != emailconsent.StatusUnsubscribed || stored.UnsubscribeSource != emailconsent.UnsubscribeOneClick {
		t.Fatalf("stored = %+v", stored)
	}
	if msgs.rows[messageID].UnsubscribedAt == nil || msgs.updates != 1 {
		t.Fatal("the message is stamped once")
	}
	if _, err := svc.Unsubscribe(context.Background(), token, emailconsent.UnsubscribeLink); err != nil {
		t.Fatal(err)
	}
	if msgs.updates != 1 || subs.rows[key("ws", "ana@example.com")].UnsubscribeSource != emailconsent.UnsubscribeOneClick {
		t.Fatal("a repeated unsubscribe changes nothing")
	}
}

func TestUnsubscribeRefusesBadTokensAndSources(t *testing.T) {
	svc, subs, _, codec := fixture(t)
	if _, err := svc.Unsubscribe(context.Background(), "garbage", emailconsent.UnsubscribeOneClick); !errors.Is(err, emailconsent.ErrUnsubscribeTokenInvalid) {
		t.Fatalf("err = %v", err)
	}
	reply, _ := codec.Seal(emailtoken.PurposeReply, messageID)
	if _, err := svc.Unsubscribe(context.Background(), reply, emailconsent.UnsubscribeOneClick); !errors.Is(err, emailconsent.ErrUnsubscribeTokenInvalid) {
		t.Fatalf("a reply token must not unsubscribe, err = %v", err)
	}
	unknown, _ := codec.Seal(emailtoken.PurposeUnsubscribe, "11111111-2222-4333-8444-555555555555")
	if _, err := svc.Unsubscribe(context.Background(), unknown, emailconsent.UnsubscribeOneClick); !errors.Is(err, emailconsent.ErrUnsubscribeTokenInvalid) {
		t.Fatalf("err = %v", err)
	}
	token, _ := codec.Seal(emailtoken.PurposeUnsubscribe, messageID)
	if _, err := svc.Unsubscribe(context.Background(), token, emailconsent.UnsubscribeComplaint); !errors.Is(err, emailconsent.ErrUnsubscribeSourceInvalid) {
		t.Fatalf("err = %v", err)
	}
	if svc.CheckToken(token) != nil || !errors.Is(svc.CheckToken("nope"), emailconsent.ErrUnsubscribeTokenInvalid) {
		t.Fatal("CheckToken disagrees with Open")
	}
	if len(subs.rows) != 0 {
		t.Fatal("refused requests store nothing")
	}
}

func TestUnsubscribeWithoutASubscriptionStillRecordsTheOptOut(t *testing.T) {
	svc, subs, _, _ := fixture(t)
	if err := svc.UnsubscribeMailto(context.Background(), messageID); err != nil {
		t.Fatal(err)
	}
	stored := subs.rows[key("ws", "ana@example.com")]
	if stored.Status != emailconsent.StatusUnsubscribed || stored.UnsubscribeSource != emailconsent.UnsubscribeMailto || stored.LeadID != "lead-ana" {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestComplaintAndOperatorUnsubscribe(t *testing.T) {
	svc, subs, _, _ := fixture(t)
	if err := svc.UnsubscribeByComplaint(context.Background(), "ws", "bia@example.com", messageID); err != nil {
		t.Fatal(err)
	}
	if subs.rows[key("ws", "bia@example.com")].UnsubscribeSource != emailconsent.UnsubscribeComplaint {
		t.Fatal("complaints unsubscribe")
	}
	got, err := svc.UnsubscribeAddress(context.Background(), "ws", " Caio@Example.com ")
	if err != nil || got.UnsubscribeSource != emailconsent.UnsubscribeOperator || got.Address != "caio@example.com" {
		t.Fatalf("got = %+v err = %v", got, err)
	}
	if _, err := svc.UnsubscribeAddress(context.Background(), "ws", "nope"); !errors.Is(err, emailconsent.ErrAddressInvalid) {
		t.Fatalf("err = %v", err)
	}
}
