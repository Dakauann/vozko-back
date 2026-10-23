package emailapi_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailapi"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	es "vozko/domain/emailsending"
	"vozko/domain/media"
	balance_usecase "vozko/usecases/balance"
)

const (
	wsID     = "ws-1"
	senderID = "0b5f2f3c-6b7d-4e5f-9a0b-1c2d3e4f5a6b"
	domainID = "dom-1"
)

var testNow = time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)

type memSends struct {
	mu       sync.Mutex
	sends    map[string]*emailapi.Send
	byKey    map[string]string
	messages map[string][]emailapi.Message
	failMark error
}

func newMemSends() *memSends {
	return &memSends{sends: map[string]*emailapi.Send{}, byKey: map[string]string{}, messages: map[string][]emailapi.Message{}}
}

func (m *memSends) Claim(_ context.Context, send emailapi.Send, messages []emailapi.Message, _ []emailapi.Tag) (emailapi.ClaimResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := send.WorkspaceID + "|" + send.IdempotencyKey
	if id, ok := m.byKey[key]; ok {
		return emailapi.ClaimResult{Send: *m.sends[id]}, nil
	}
	s := send
	m.sends[s.ID] = &s
	m.byKey[key] = s.ID
	m.messages[s.ID] = append([]emailapi.Message(nil), messages...)
	return emailapi.ClaimResult{Send: send, Created: true, Messages: messages}, nil
}

func (m *memSends) TakeLease(_ context.Context, ws, id string, now, until time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sends[id]
	if s == nil || s.WorkspaceID != ws || s.LeaseHeld(now) {
		return false, nil
	}
	s.LeaseUntil = &until
	return true, nil
}

func (m *memSends) ReleaseLease(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends[id].LeaseUntil = nil
	return nil
}

func (m *memSends) Messages(_ context.Context, id string) ([]emailapi.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]emailapi.Message(nil), m.messages[id]...), nil
}

func (m *memSends) OpenRound(_ context.Context, id string, round int, units, unit, cost int64, positions []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sends[id]
	if s.Round != round-1 || s.RoundOpen() {
		return emailapi.ErrSendInProgress
	}
	s.Round, s.RoundUnits, s.RoundUnitMicros, s.RoundUnitCostMicros, s.RoundSettled = round, units, unit, cost, false
	for i := range m.messages[id] {
		for _, p := range positions {
			if m.messages[id][i].Position == p {
				m.messages[id][i].Round, m.messages[id][i].Status, m.messages[id][i].ErrorCode = round, emailapi.StatusPending, ""
			}
		}
	}
	return nil
}

func (m *memSends) SettleRound(_ context.Context, id string, round int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sends[id].Round == round {
		m.sends[id].RoundSettled = true
	}
	return nil
}

func (m *memSends) MarkSent(_ context.Context, id string, position int, messageID string, status emailapi.MessageStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failMark != nil {
		return m.failMark
	}
	for i := range m.messages[id] {
		if m.messages[id][i].Position == position {
			m.messages[id][i].MessageID, m.messages[id][i].Status, m.messages[id][i].ErrorCode = messageID, status, ""
		}
	}
	return nil
}

func (m *memSends) MarkRefused(_ context.Context, id string, positions []int, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.messages[id] {
		for _, p := range positions {
			if m.messages[id][i].Position == p && m.messages[id][i].MessageID == "" {
				m.messages[id][i].Status, m.messages[id][i].ErrorCode = emailapi.StatusRefused, code
			}
		}
	}
	return nil
}

func (m *memSends) MarkStatus(context.Context, string, emailapi.MessageStatus, string, time.Time) (bool, error) {
	return false, nil
}

func (m *memSends) only(t *testing.T) (emailapi.Send, []emailapi.Message) {
	t.Helper()
	if len(m.sends) != 1 {
		t.Fatalf("sends = %d", len(m.sends))
	}
	for id, s := range m.sends {
		return *s, m.messages[id]
	}
	return emailapi.Send{}, nil
}

type fakeGateway struct {
	mu       sync.Mutex
	requests []es.SendRequest
	contents []es.Content
	refuse   map[emailaddress.Address]error
	next     int
	sent     map[string]es.Message
}

func (g *fakeGateway) Send(_ context.Context, req es.SendRequest, compose es.ComposeFunc) (*es.Message, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.refuse[req.To]; err != nil {
		return nil, err
	}
	content, err := compose(es.Envelope{})
	if err != nil {
		return nil, err
	}
	g.next++
	g.requests = append(g.requests, req)
	g.contents = append(g.contents, content)
	msg := es.Message{ID: fmt.Sprintf("msg-%d", g.next), WorkspaceID: req.WorkspaceID, RunKind: req.RunKind, RunID: req.RunID, RecipientID: req.RecipientID, Address: req.To, Status: es.MessageSent}
	if g.sent == nil {
		g.sent = map[string]es.Message{}
	}
	g.sent[msg.ID] = msg
	return &msg, nil
}

func (g *fakeGateway) ListByRun(_ context.Context, ws string, kind es.RunKind, runID string) ([]es.Message, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []es.Message
	for _, m := range g.sent {
		if m.WorkspaceID == ws && m.RunKind == kind && m.RunID == runID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type fakeBilling struct {
	mu        sync.Mutex
	wallet    int64
	unit      int64
	chunk     int64
	charges   map[string]quote
	refunds   map[string]int64
	quoteErr  error
	refundErr error
}

func newBilling(wallet int64) *fakeBilling {
	return &fakeBilling{wallet: wallet, unit: 583, chunk: 102, charges: map[string]quote{}, refunds: map[string]int64{}}
}

func (b *fakeBilling) QuoteSend(_ context.Context, _ string, n, encoded int64) (quote, error) {
	if b.quoteErr != nil {
		return quote{}, b.quoteErr
	}
	unit := b.unit + b.chunk*es.AttachmentChunks(encoded)
	return quote{Units: n, UnitMicros: unit, UnitCostMicros: 100, TotalMicros: n * unit}, nil
}

func (b *fakeBilling) ChargeSend(_ context.Context, _ string, ref string, q quote) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.charges[ref]; ok {
		return false, nil
	}
	if b.wallet < q.TotalMicros {
		return false, &balance_usecase.InsufficientEmailBalanceError{MissingMicros: q.TotalMicros - b.wallet}
	}
	b.wallet -= q.TotalMicros
	b.charges[ref] = q
	return true, nil
}

func (b *fakeBilling) RefundSend(_ context.Context, _ string, ref string, q quote, units int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refundErr != nil {
		return b.refundErr
	}
	if _, ok := b.charges[ref]; !ok {
		return errors.New("refund without a charge")
	}
	if _, done := b.refunds[ref]; done {
		return nil
	}
	b.refunds[ref] = units
	b.wallet += units * q.UnitMicros
	return nil
}

func (b *fakeBilling) SendCharged(_ context.Context, ref string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.charges[ref]
	return ok, nil
}

type fakeDesigns map[string]emaildesign.Document

func (f fakeDesigns) Get(_ context.Context, ws, id string) (*emailcampaign.Design, error) {
	doc, ok := f[ws+"/"+id]
	if !ok {
		return nil, emailcampaign.ErrDesignNotFound
	}
	return &emailcampaign.Design{ID: id, WorkspaceID: ws, Document: doc}, nil
}

type fakeSenders struct{ senders []es.Sender }

func (f fakeSenders) Get(_ context.Context, ws, id string) (*es.Sender, error) {
	for _, s := range f.senders {
		if s.ID == id && s.WorkspaceID == ws {
			c := s
			return &c, nil
		}
	}
	return nil, es.ErrSenderNotFound
}

func (f fakeSenders) ListByWorkspace(_ context.Context, ws string) ([]es.Sender, error) {
	return f.senders, nil
}

type fakeDomains struct{ domains []es.SendingDomain }

func (f *fakeDomains) ListByWorkspace(context.Context, string) ([]es.SendingDomain, error) {
	return f.domains, nil
}

type fakeSuppressions map[emailaddress.Address]es.SuppressReason

func (f fakeSuppressions) Suppressed(_ context.Context, _ string, addrs []emailaddress.Address) (map[emailaddress.Address]es.SuppressReason, error) {
	out := map[emailaddress.Address]es.SuppressReason{}
	for _, a := range addrs {
		if r, ok := f[a]; ok {
			out[a] = r
		}
	}
	return out, nil
}

type fakeFiles struct {
	files map[string]media.RemoteFile
	err   error
}

func (f fakeFiles) Download(_ context.Context, url string, maxBytes int64) (media.RemoteFile, error) {
	if f.err != nil {
		return media.RemoteFile{}, f.err
	}
	file, ok := f.files[url]
	if !ok {
		return media.RemoteFile{}, media.ErrRemoteFileMissing
	}
	if int64(len(file.Data)) > maxBytes {
		return media.RemoteFile{}, media.ErrMediaTooLarge
	}
	return file, nil
}

type nopLog struct{}

func (nopLog) List(context.Context, emailapi.LogQuery) ([]emailapi.LogEntry, error) { return nil, nil }
func (nopLog) Get(context.Context, string, string) (*emailapi.LogEntry, error) {
	return nil, emailapi.ErrMessageNotFound
}
func (nopLog) GetByMessageID(context.Context, string) (*emailapi.LogEntry, error) {
	return nil, emailapi.ErrMessageNotFound
}
func (nopLog) Events(context.Context, string) ([]emailapi.EventView, error) { return nil, nil }

type harness struct {
	sends        *memSends
	gateway      *fakeGateway
	billing      *fakeBilling
	designs      fakeDesigns
	domains      *fakeDomains
	suppressions fakeSuppressions
	files        fakeFiles
	logs         []string
	ids          int
	now          time.Time
}

func newHarness() *harness {
	return &harness{
		sends:        newMemSends(),
		gateway:      &fakeGateway{refuse: map[emailaddress.Address]error{}},
		billing:      newBilling(1_000_000),
		designs:      fakeDesigns{},
		domains:      &fakeDomains{domains: []es.SendingDomain{{ID: domainID, WorkspaceID: wsID, Domain: "acme.com", Kind: es.DomainOwn, Status: es.DomainVerified}}},
		suppressions: fakeSuppressions{},
		files:        fakeFiles{files: map[string]media.RemoteFile{}},
		now:          testNow,
	}
}

func (h *harness) service(t *testing.T) *Service {
	t.Helper()
	svc, err := New(Deps{
		Sends: h.sends, Log: nopLog{}, Runs: h.gateway, Gateway: h.gateway, Billing: h.billing, Designs: h.designs,
		Senders: fakeSenders{senders: []es.Sender{{ID: senderID, WorkspaceID: wsID, DomainID: domainID, LocalPart: "no-reply", DisplayName: "Acme"}}},
		Domains: h.domains, Suppressions: h.suppressions, Files: h.files,
		Now:   func() time.Time { return h.now },
		NewID: func() string { h.ids++; return fmt.Sprintf("00000000-0000-4000-8000-%012d", h.ids) },
		Logf:  func(format string, args ...any) { h.logs = append(h.logs, fmt.Sprintf(format, args...)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func request(to ...string) emailapi.Request {
	return emailapi.Request{
		WorkspaceID: wsID, Origin: emailapi.OriginAPI, APIKeyID: "key-1", From: "no-reply@acme.com", To: to,
		Content:        emailapi.Content{Subject: "Seu código", HTML: "<p>123</p>", Text: "123"},
		IdempotencyKey: "reset-1",
	}
}

func joinLogs(h *harness) string { return strings.Join(h.logs, "\n") }
