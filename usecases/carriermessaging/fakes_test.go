package carriermessaging_usecase

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

var t0 = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

type memSenders struct {
	mu   sync.Mutex
	rows map[string]*rcs.Sender
}

func (m *memSenders) Create(_ context.Context, s *rcs.Sender) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		s.ID = fmt.Sprintf("sender-%d", len(m.rows)+1)
	}
	c := *s
	m.rows[s.ID] = &c
	return nil
}

func (m *memSenders) Update(_ context.Context, s *rcs.Sender) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[s.ID]; !ok {
		return rcs.ErrSenderNotFound
	}
	c := *s
	m.rows[s.ID] = &c
	return nil
}

func (m *memSenders) FindByID(_ context.Context, workspaceID, id string) (*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[id]
	if !ok || s.WorkspaceID != workspaceID {
		return nil, rcs.ErrSenderNotFound
	}
	c := *s
	return &c, nil
}

func (m *memSenders) FindByIDUnscoped(_ context.Context, id string) (*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[id]
	if !ok {
		return nil, rcs.ErrSenderNotFound
	}
	c := *s
	return &c, nil
}

func (m *memSenders) FindByProfileID(_ context.Context, provider, profileID string) (*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.rows {
		if s.Provider == provider && s.ProviderProfileID == profileID {
			c := *s
			return &c, nil
		}
	}
	return nil, rcs.ErrSenderNotFound
}

func (m *memSenders) ListByWorkspace(_ context.Context, workspaceID string) ([]*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*rcs.Sender
	for _, s := range m.rows {
		if s.WorkspaceID == workspaceID {
			c := *s
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memSenders) ListAwaitingProvider(_ context.Context, limit int) ([]*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*rcs.Sender
	for _, s := range m.rows {
		if s.Status == rcs.SenderSubmitted || s.Status == rcs.SenderAwaitingConfirmation || s.Status == rcs.SenderLaunched || s.Status == rcs.SenderPaused {
			c := *s
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memSenders) ListAll(_ context.Context, status *rcs.SenderStatus) ([]*rcs.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*rcs.Sender
	for _, s := range m.rows {
		if status == nil || s.Status == *status {
			c := *s
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memSenders) Delete(_ context.Context, workspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}

type memMessages struct {
	mu   sync.Mutex
	rows map[string]*carriermessaging.Message
}

func (m *memMessages) Create(_ context.Context, msg *carriermessaging.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *msg
	m.rows[msg.ID] = &c
	return nil
}

func (m *memMessages) FindByID(_ context.Context, id string) (*carriermessaging.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.rows[id]
	if !ok {
		return nil, carriermessaging.ErrMessageNotFound
	}
	c := *msg
	return &c, nil
}

func (m *memMessages) byReference(ref string) *carriermessaging.Message {
	for _, msg := range m.rows {
		if msg.Reference == ref {
			return msg
		}
	}
	return nil
}

func (m *memMessages) FindByReference(_ context.Context, ref string) (*carriermessaging.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := m.byReference(ref)
	if msg == nil {
		return nil, carriermessaging.ErrMessageNotFound
	}
	c := *msg
	return &c, nil
}

func (m *memMessages) Advance(_ context.Context, ref string, next carriermessaging.Status, code string, at time.Time) (*carriermessaging.Message, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := m.byReference(ref)
	if msg == nil {
		return nil, false, carriermessaging.ErrMessageNotFound
	}
	advanced, moved := msg.Advance(next, code, at)
	if moved {
		*msg = advanced
	}
	c := *msg
	return &c, moved, nil
}

func (m *memMessages) LinkFallback(_ context.Context, id, fallbackID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.rows[id]
	if !ok {
		return false, carriermessaging.ErrMessageNotFound
	}
	if msg.FallbackMessageID != "" {
		return false, nil
	}
	msg.FallbackMessageID = fallbackID
	return true, nil
}

func (m *memMessages) ListOverdue(_ context.Context, now time.Time, limit int) ([]*carriermessaging.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*carriermessaging.Message
	for _, msg := range m.rows {
		if msg.Overdue(now) {
			c := *msg
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memMessages) CountMarketingSince(_ context.Context, ws, phone string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, msg := range m.rows {
		if msg.WorkspaceID == ws && msg.Phone == phone && msg.Marketing && !msg.CreatedAt.Before(since) && msg.Status != carriermessaging.StatusRejected {
			n++
		}
	}
	return n, nil
}

func (m *memMessages) CountRunKindSince(_ context.Context, ws string, kind carriermessaging.RunKind, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, msg := range m.rows {
		if msg.WorkspaceID == ws && msg.RunKind == kind && !msg.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (m *memMessages) LatestDeliveredTo(_ context.Context, ws, phone string, since time.Time) (*carriermessaging.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *carriermessaging.Message
	for _, msg := range m.rows {
		if msg.WorkspaceID == ws && msg.Phone == phone && msg.Status.Landed() && msg.DeliveredAt != nil && !msg.DeliveredAt.Before(since) {
			if best == nil || msg.DeliveredAt.After(*best.DeliveredAt) {
				best = msg
			}
		}
	}
	if best == nil {
		return nil, nil
	}
	c := *best
	return &c, nil
}

func (m *memMessages) all() []*carriermessaging.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*carriermessaging.Message, 0, len(m.rows))
	for _, msg := range m.rows {
		c := *msg
		out = append(out, &c)
	}
	return out
}

type memEvents struct {
	mu   sync.Mutex
	rows []*carriermessaging.MessageEvent
	keys map[string]bool
}

func (m *memEvents) Record(_ context.Context, e *carriermessaging.MessageEvent) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys[e.DedupKey] {
		return false, nil
	}
	m.keys[e.DedupKey] = true
	c := *e
	m.rows = append(m.rows, &c)
	return true, nil
}

func (m *memEvents) CountSince(_ context.Context, ws, phone string, kind carriermessaging.EventKind, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.rows {
		if e.WorkspaceID == ws && e.Phone == phone && e.Kind == kind && !e.OccurredAt.Before(since) {
			n++
		}
	}
	return n, nil
}

type memSuppressions struct {
	mu   sync.Mutex
	rows map[string]carriermessaging.Suppression
}

func (m *memSuppressions) Add(_ context.Context, s carriermessaging.Suppression) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := s.WorkspaceID + "|" + s.Phone
	if _, ok := m.rows[key]; ok {
		return false, nil
	}
	m.rows[key] = s
	return true, nil
}

func (m *memSuppressions) Remove(_ context.Context, ws, phone string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, ws+"|"+phone)
	return nil
}

func (m *memSuppressions) IsSuppressed(_ context.Context, ws, phone string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[ws+"|"+phone]
	return ok, nil
}

func (m *memSuppressions) SuppressedAmong(_ context.Context, ws string, phones []string) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for _, p := range phones {
		if _, ok := m.rows[ws+"|"+p]; ok {
			out[p] = true
		}
	}
	return out, nil
}

func (m *memSuppressions) List(_ context.Context, ws string, limit, offset int) ([]carriermessaging.Suppression, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []carriermessaging.Suppression
	for _, s := range m.rows {
		if s.WorkspaceID == ws {
			out = append(out, s)
		}
	}
	return out, len(out), nil
}

type memSettings struct {
	rows map[string]carriermessaging.Settings
}

func (m *memSettings) Get(_ context.Context, ws string) (*carriermessaging.Settings, error) {
	s, ok := m.rows[ws]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (m *memSettings) Put(_ context.Context, s carriermessaging.Settings) error {
	m.rows[s.WorkspaceID] = s
	return nil
}

type memContacts struct {
	mu   sync.Mutex
	rows map[string]*carriermessaging.Contact
}

func (m *memContacts) FindOrCreate(_ context.Context, ws, phone, name string) (*carriermessaging.Contact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.rows {
		if c.WorkspaceID == ws && c.Phone == phone {
			cp := *c
			return &cp, nil
		}
	}
	c := &carriermessaging.Contact{ID: fmt.Sprintf("contact-%d", len(m.rows)+1), WorkspaceID: ws, Phone: phone, DisplayName: name}
	m.rows[c.ID] = c
	cp := *c
	return &cp, nil
}

func (m *memContacts) FindByID(_ context.Context, id string) (*carriermessaging.Contact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return nil, carriermessaging.ErrContactNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *memContacts) FindByIDs(ctx context.Context, ids []string) ([]*carriermessaging.Contact, error) {
	var out []*carriermessaging.Contact
	for _, id := range ids {
		if c, err := m.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memContacts) SetLead(_ context.Context, id, leadID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return carriermessaging.ErrContactNotFound
	}
	c.LeadID = &leadID
	return nil
}

func (m *memContacts) MarkFlooding(_ context.Context, id string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return false, carriermessaging.ErrContactNotFound
	}
	if c.FloodingAt != nil {
		return false, nil
	}
	c.FloodingAt = &at
	return true, nil
}

type memConversations struct {
	mu     sync.Mutex
	prefix string
	rows   map[string]*carriermessaging.Conversation
}

func (m *memConversations) FindOrCreate(_ context.Context, ws, senderID, contactID string) (*carriermessaging.Conversation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.rows {
		if c.WorkspaceID == ws && c.SenderID == senderID && c.ContactID == contactID {
			cp := *c
			return &cp, false, nil
		}
	}
	c := &carriermessaging.Conversation{ID: fmt.Sprintf("%s-%d", m.prefix, len(m.rows)+1), WorkspaceID: ws, SenderID: senderID, ContactID: contactID}
	m.rows[c.ID] = c
	cp := *c
	return &cp, true, nil
}

func (m *memConversations) FindByID(_ context.Context, id string) (*carriermessaging.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return nil, carriermessaging.ErrConversationNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *memConversations) FindByIDs(ctx context.Context, ids []string) ([]*carriermessaging.Conversation, error) {
	var out []*carriermessaging.Conversation
	for _, id := range ids {
		if c, err := m.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memConversations) RecordInbound(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return carriermessaging.ErrConversationNotFound
	}
	c.LastMessageAt, c.LastCustomerMessageAt = &at, &at
	return nil
}

func (m *memConversations) RecordOutbound(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return carriermessaging.ErrConversationNotFound
	}
	c.LastMessageAt, c.LastAgentMessageAt = &at, &at
	return nil
}

func (m *memConversations) OpenBilledConversation(_ context.Context, id string, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return false, carriermessaging.ErrConversationNotFound
	}
	if c.BilledConversationOpen(at) {
		return false, nil
	}
	c.ConversationOpenedAt = &at
	return true, nil
}

type fakeProvider struct {
	mu       sync.Mutex
	sends    []carriermessaging.SendRequest
	outcome  carriermessaging.SendOutcome
	err      error
	sms      []carriermessaging.SMSRequest
	smsErr   error
	smsOut   carriermessaging.SendOutcome
	submits  []rcs.Sender
	status   carriermessaging.SenderStatusReport
	statusOK bool
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Send(_ context.Context, req carriermessaging.SendRequest) (carriermessaging.SendOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sends = append(p.sends, req)
	return p.outcome, p.err
}

func (p *fakeProvider) SendSMS(_ context.Context, req carriermessaging.SMSRequest) (carriermessaging.SendOutcome, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sms = append(p.sms, req)
	return p.smsOut, p.smsErr
}

func (p *fakeProvider) SubmitSender(_ context.Context, s rcs.Sender) (carriermessaging.SenderSubmission, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits = append(p.submits, s)
	return carriermessaging.SenderSubmission{RequestID: "req-1", ProfileID: "profile-1"}, p.err
}

func (p *fakeProvider) SenderStatus(_ context.Context, _ rcs.Sender) (carriermessaging.SenderStatusReport, error) {
	return p.status, p.err
}

type fakeBalance struct {
	micros int64
	err    error
}

func (b *fakeBalance) GetBalance(string) (int64, error) { return b.micros, b.err }

type fakeSpend struct{ micros int64 }

func (s *fakeSpend) SpendSince(context.Context, string, time.Time) (int64, error) {
	return s.micros, nil
}

type fakeLinks struct{}

func (fakeLinks) TrackedURL(messageID string, key rcs.ButtonKey) (string, error) {
	return "https://api.example.com/public/rcs/r/" + messageID + "." + key.Postback(), nil
}

type fakeLeads struct {
	leads map[string]*lead.Lead
}

func (f *fakeLeads) EnsureLead(_ context.Context, ws, phone, name string) (*lead.Lead, error) {
	if l, ok := f.leads[phone]; ok {
		return l, nil
	}
	l := &lead.Lead{ID: "lead-" + phone, WorkspaceID: ws, Number: phone, Name: name}
	f.leads[phone] = l
	return l, nil
}

type fakeTranscript struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
}

func (f *fakeTranscript) Record(_ context.Context, r conversation.MessageHistoryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
	return nil
}

type statusUpdate struct {
	providerID string
	status     conversation.DeliveryStatus
}

type fakeTranscriptStatus struct {
	mu      sync.Mutex
	updates []statusUpdate
}

func (f *fakeTranscriptStatus) UpdateDeliveryStatusWithReason(id string, status conversation.DeliveryStatus, _ int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, statusUpdate{id, status})
	return nil
}

type fakeAnnouncer struct {
	mu     sync.Mutex
	events []statusUpdate
}

func (f *fakeAnnouncer) BroadcastMessageStatus(_, _, messageID string, status conversation.DeliveryStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, statusUpdate{messageID, status})
}

type fakeTimezones struct{}

func (fakeTimezones) Timezone(context.Context, string) (string, error) {
	return "America/Sao_Paulo", nil
}

type fakeWhatsApp struct {
	mu     sync.Mutex
	starts []WhatsAppStart
	err    error
}

func (f *fakeWhatsApp) StartTemplate(_ context.Context, in WhatsAppStart) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, in)
	if f.err != nil {
		return "", f.err
	}
	return "wamid-" + in.IdempotencyKey, nil
}

type fakeNotices struct {
	mu       sync.Mutex
	flooding []string
	states   []rcs.SenderStatus
}

func (f *fakeNotices) ContactFlooding(_ context.Context, _ string, _ carriermessaging.Channel, c carriermessaging.Contact, _ int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flooding = append(f.flooding, c.Phone)
}

func (f *fakeNotices) SenderStateChanged(_ context.Context, s rcs.Sender) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, s.Status)
}

type fakeInbox struct {
	mu          sync.Mutex
	assignments []string
	dispatched  []AutomationInput
}

func (f *fakeInbox) EnsureAssignment(_ context.Context, _ string, _ carriermessaging.Channel, conversationID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assignments = append(f.assignments, conversationID)
	return nil
}

func (f *fakeInbox) DispatchAutomation(_ context.Context, in AutomationInput) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatched = append(f.dispatched, in)
}

func (fakeLinks) Verify(token string) (string, rcs.ButtonKey, error) {
	id, code, ok := strings.Cut(token, ".")
	if !ok {
		return "", rcs.ButtonKey{}, ErrLinkInvalid
	}
	key, ok := rcs.ParsePostback(code)
	if !ok {
		return "", rcs.ButtonKey{}, ErrLinkInvalid
	}
	return id, key, nil
}

func (m *memMessages) LatestSMSTo(_ context.Context, phone string, since time.Time) (*carriermessaging.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *carriermessaging.Message
	for _, msg := range m.rows {
		if msg.Phone == phone && msg.Step == carriermessaging.StepSMS && !msg.CreatedAt.Before(since) {
			if best == nil || msg.CreatedAt.After(best.CreatedAt) {
				best = msg
			}
		}
	}
	if best == nil {
		return nil, nil
	}
	c := *best
	return &c, nil
}

type memRoutes struct {
	mu   sync.Mutex
	rows map[string]*sms.Route
}

func (m *memRoutes) Create(_ context.Context, r *sms.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = fmt.Sprintf("route-%d", len(m.rows)+1)
	}
	c := *r
	m.rows[r.ID] = &c
	return nil
}

func (m *memRoutes) Update(_ context.Context, r *sms.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[r.ID]; !ok {
		return sms.ErrRouteNotFound
	}
	c := *r
	m.rows[r.ID] = &c
	return nil
}

func (m *memRoutes) FindByID(_ context.Context, ws, id string) (*sms.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.WorkspaceID != ws {
		return nil, sms.ErrRouteNotFound
	}
	c := *r
	return &c, nil
}

func (m *memRoutes) FindByIDUnscoped(_ context.Context, id string) (*sms.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return nil, sms.ErrRouteNotFound
	}
	c := *r
	return &c, nil
}

func (m *memRoutes) FindByNumber(_ context.Context, number string) (*sms.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Kind == sms.RouteDedicated && r.Number == number {
			c := *r
			return &c, nil
		}
	}
	return nil, sms.ErrRouteNotFound
}

func (m *memRoutes) ListByWorkspace(_ context.Context, ws string) ([]*sms.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sms.Route
	for _, r := range m.rows {
		if r.WorkspaceID == ws {
			c := *r
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memRoutes) ListAll(_ context.Context) ([]*sms.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sms.Route
	for _, r := range m.rows {
		c := *r
		out = append(out, &c)
	}
	return out, nil
}

func (m *memRoutes) Delete(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}
