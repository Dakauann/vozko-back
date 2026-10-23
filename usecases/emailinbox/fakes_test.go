package emailinbox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	conv_event "vozko/domain/conversation_event"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	"vozko/domain/shared"
	balance_usecase "vozko/usecases/balance"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/shared/automationrefs"
)

const testSecret = "0123456789abcdef0123456789abcdef-test"

type fakeRaw struct {
	objects   map[string][]byte
	deleted   []string
	deleteErr error
	err       error
}

func (f *fakeRaw) Fetch(_ context.Context, key string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	raw, ok := f.objects[key]
	if !ok {
		return nil, errors.New("no object")
	}
	return raw, nil
}

type fakeParser struct {
	msg em.InboundMessage
	err error
}

func (f fakeParser) Parse([]byte) (em.InboundMessage, error) { return f.msg, f.err }

type fakeSent struct {
	byID map[string]*emailsending.Message
}

func (f *fakeSent) Get(_ context.Context, id string) (*emailsending.Message, error) {
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return nil, emailsending.ErrMessageNotFound
}

type fakeDomains struct {
	byReply map[string]*emailsending.SendingDomain
	byID    map[string]*emailsending.SendingDomain
}

func (f *fakeDomains) GetByReplyDomain(_ context.Context, replyDomain string) (*emailsending.SendingDomain, error) {
	if d, ok := f.byReply[replyDomain]; ok {
		return d, nil
	}
	return nil, emailsending.ErrDomainNotFound
}

func (f *fakeDomains) Get(_ context.Context, workspaceID, id string) (*emailsending.SendingDomain, error) {
	if d, ok := f.byID[id]; ok && d.WorkspaceID == workspaceID {
		return d, nil
	}
	return nil, emailsending.ErrDomainNotFound
}

type fakeSenders struct {
	all []emailsending.Sender
}

func (f *fakeSenders) Get(_ context.Context, workspaceID, id string) (*emailsending.Sender, error) {
	for i := range f.all {
		if f.all[i].ID == id && f.all[i].WorkspaceID == workspaceID {
			s := f.all[i]
			return &s, nil
		}
	}
	return nil, emailsending.ErrSenderNotFound
}

func (f *fakeSenders) GetDefault(_ context.Context, workspaceID string) (*emailsending.Sender, error) {
	for i := range f.all {
		if f.all[i].WorkspaceID == workspaceID && f.all[i].IsDefault {
			s := f.all[i]
			return &s, nil
		}
	}
	return nil, emailsending.ErrNoDefaultSender
}

func (f *fakeSenders) ListByWorkspace(_ context.Context, workspaceID string) ([]emailsending.Sender, error) {
	var out []emailsending.Sender
	for _, s := range f.all {
		if s.WorkspaceID == workspaceID {
			out = append(out, s)
		}
	}
	return out, nil
}

type fakeUnsubscriber struct {
	calls []string
	err   error
}

func (f *fakeUnsubscriber) UnsubscribeMailto(_ context.Context, messageID string) error {
	f.calls = append(f.calls, messageID)
	return f.err
}

type fakeInboxes struct {
	mu   sync.Mutex
	byID map[string]*em.Inbox
}

func newFakeInboxes() *fakeInboxes { return &fakeInboxes{byID: map[string]*em.Inbox{}} }

func (f *fakeInboxes) Ensure(_ context.Context, inbox em.Inbox) (*em.Inbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.byID[inbox.ID]; ok {
		if existing.WorkspaceID != inbox.WorkspaceID {
			return nil, em.ErrInboxNotFound
		}
		existing.Name, existing.Address = inbox.Name, inbox.Address
		copy := *existing
		return &copy, nil
	}
	stored := inbox
	f.byID[inbox.ID] = &stored
	copy := stored
	return &copy, nil
}

func (f *fakeInboxes) FindByID(_ context.Context, workspaceID, id string) (*em.Inbox, error) {
	if i, ok := f.byID[id]; ok && i.WorkspaceID == workspaceID {
		copy := *i
		return &copy, nil
	}
	return nil, em.ErrInboxNotFound
}

func (f *fakeInboxes) FindByIDUnscoped(_ context.Context, id string) (*em.Inbox, error) {
	if i, ok := f.byID[id]; ok {
		copy := *i
		return &copy, nil
	}
	return nil, em.ErrInboxNotFound
}

func (f *fakeInboxes) FindByIDsUnscoped(ctx context.Context, ids []string) ([]*em.Inbox, error) {
	var out []*em.Inbox
	for _, id := range ids {
		if i, err := f.FindByIDUnscoped(ctx, id); err == nil {
			out = append(out, i)
		}
	}
	return out, nil
}

func (f *fakeInboxes) ListByWorkspace(_ context.Context, workspaceID string) ([]*em.Inbox, error) {
	var out []*em.Inbox
	for _, i := range f.byID {
		if i.WorkspaceID == workspaceID {
			copy := *i
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (f *fakeInboxes) Update(_ context.Context, inbox *em.Inbox) error {
	existing, ok := f.byID[inbox.ID]
	if !ok || existing.WorkspaceID != inbox.WorkspaceID {
		return em.ErrInboxNotFound
	}
	stored := *inbox
	f.byID[inbox.ID] = &stored
	return nil
}

type fakeContacts struct {
	byID map[string]*em.Contact
}

func newFakeContacts() *fakeContacts { return &fakeContacts{byID: map[string]*em.Contact{}} }

func (f *fakeContacts) FindOrCreate(ctx context.Context, in em.FindOrCreateContactInput) (*em.Contact, error) {
	if c, err := f.FindByAddress(ctx, in.WorkspaceID, in.Address); err == nil {
		return c, nil
	}
	c := &em.Contact{ID: uuid.NewString(), WorkspaceID: in.WorkspaceID, Address: in.Address, DisplayName: in.DisplayName, LeadID: in.LeadID}
	f.byID[c.ID] = c
	copy := *c
	return &copy, nil
}

func (f *fakeContacts) FindByID(_ context.Context, id string) (*em.Contact, error) {
	if c, ok := f.byID[id]; ok {
		copy := *c
		return &copy, nil
	}
	return nil, em.ErrContactNotFound
}

func (f *fakeContacts) FindByIDs(ctx context.Context, ids []string) ([]*em.Contact, error) {
	var out []*em.Contact
	for _, id := range ids {
		if c, err := f.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeContacts) FindByAddress(_ context.Context, workspaceID string, address emailaddress.Address) (*em.Contact, error) {
	for _, c := range f.byID {
		if c.WorkspaceID == workspaceID && c.Address == address {
			copy := *c
			return &copy, nil
		}
	}
	return nil, em.ErrContactNotFound
}

func (f *fakeContacts) UpdateDisplayName(_ context.Context, id, displayName string) error {
	f.byID[id].DisplayName = displayName
	return nil
}

func (f *fakeContacts) LinkLead(_ context.Context, id, leadID string) error {
	if f.byID[id].LeadID == nil {
		f.byID[id].LeadID = &leadID
	}
	return nil
}

type fakeConversations struct {
	byID      map[string]*em.Conversation
	inbound   []string
	outbound  []string
	threads   map[string]em.Thread
	findError error
}

func newFakeConversations() *fakeConversations {
	return &fakeConversations{byID: map[string]*em.Conversation{}, threads: map[string]em.Thread{}}
}

func (f *fakeConversations) add(c *em.Conversation) *em.Conversation {
	f.byID[c.ID] = c
	return c
}

func (f *fakeConversations) FindOrCreate(ctx context.Context, in em.FindOrCreateConversationInput) (*em.Conversation, bool, error) {
	if c, err := f.FindByContact(ctx, in.WorkspaceID, in.ContactID); err == nil {
		return c, false, nil
	}
	c := &em.Conversation{ID: uuid.NewString(), WorkspaceID: in.WorkspaceID, ContactID: in.ContactID, SenderID: in.SenderID}
	f.byID[c.ID] = c
	copy := *c
	return &copy, true, nil
}

func (f *fakeConversations) FindByID(_ context.Context, id string) (*em.Conversation, error) {
	if f.findError != nil {
		return nil, f.findError
	}
	if c, ok := f.byID[id]; ok {
		copy := *c
		return &copy, nil
	}
	return nil, em.ErrConversationNotFound
}

func (f *fakeConversations) FindByIDs(ctx context.Context, ids []string) ([]*em.Conversation, error) {
	var out []*em.Conversation
	for _, id := range ids {
		if c, err := f.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeConversations) FindByContact(_ context.Context, workspaceID, contactID string) (*em.Conversation, error) {
	for _, c := range f.byID {
		if c.WorkspaceID == workspaceID && c.ContactID == contactID {
			copy := *c
			return &copy, nil
		}
	}
	return nil, em.ErrConversationNotFound
}

func (f *fakeConversations) ApplyThread(_ context.Context, id string, thread em.Thread) error {
	f.threads[id] = thread
	c := f.byID[id]
	c.Subject, c.ThreadKey, c.LastInboundMessageID, c.References = thread.Subject, thread.ThreadKey, thread.LastInboundMessageID, thread.References
	return nil
}

func (f *fakeConversations) WorkspaceIDForEntry(_ context.Context, id string) (string, error) {
	return f.byID[id].WorkspaceID, nil
}

func (f *fakeConversations) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversations) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeConversations) RecordInbound(_ context.Context, id string, at time.Time) error {
	f.inbound = append(f.inbound, id)
	return nil
}

func (f *fakeConversations) RecordOutbound(_ context.Context, id string, at time.Time) error {
	f.outbound = append(f.outbound, id)
	return nil
}

func (f *fakeConversations) SetStatus(context.Context, string, conversation.StatusWrite) error {
	return nil
}

func (f *fakeConversations) SetAutomationEnabled(context.Context, string, *bool) error { return nil }

func (f *fakeConversations) StatusForEntry(context.Context, string) (string, error) { return "", nil }

func (f *fakeConversations) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}

type fakeLeads struct {
	byEmail map[string]*lead.Lead
}

func (f *fakeLeads) FindByEmail(workspaceID, email string) (*lead.Lead, error) {
	if l, ok := f.byEmail[workspaceID+"|"+email]; ok {
		return l, nil
	}
	return nil, lead.ErrLeadNotFound
}

type fakeHistory struct {
	conversation.MessageHistoryManager
	store   *fakeMessages
	records []conversation.MessageHistoryRecord
}

func (f *fakeHistory) Record(_ context.Context, rec conversation.MessageHistoryRecord) error {
	f.records = append(f.records, rec)
	message := rec.Message()
	external := rec.ProviderMessageID
	message.ExternalMessageID = &external
	f.store.put(message)
	return nil
}

type fakeMessages struct {
	mu    sync.Mutex
	rows  map[string]*conversation.Message
	saved []*conversation.Message
}

func newFakeMessages() *fakeMessages { return &fakeMessages{rows: map[string]*conversation.Message{}} }

func messageKey(entryType shared.EntryType, entryID, externalID string) string {
	return string(entryType) + "|" + entryID + "|" + externalID
}

func (f *fakeMessages) put(m *conversation.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[messageKey(m.EntryType, m.EntryID, *m.ExternalMessageID)] = m
}

func (f *fakeMessages) GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.rows[messageKey(entryType, entryID, externalID)]; ok {
		copy := *m
		return &copy, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (f *fakeMessages) Update(_ string, m *conversation.Message) error {
	f.saved = append(f.saved, m)
	return nil
}

type fakeMedia struct {
	inputs []conversation.StoreMediaInput
	err    error
}

func (f *fakeMedia) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.inputs = append(f.inputs, in)
	return &conversation.ConversationMedia{ID: in.ID, Type: in.Type, URL: "https://cdn/" + in.Key}, nil
}

type fakeAssignments struct {
	calls []string
}

func (f *fakeAssignments) EnsureAssignment(entryID, entryType, accountID string) string {
	f.calls = append(f.calls, entryType+"|"+entryID+"|"+accountID)
	return ""
}

type fakeDispatcher struct {
	inputs []conversation_usecase.InboundAutomationInput
}

func (f *fakeDispatcher) Dispatch(_ context.Context, in conversation_usecase.InboundAutomationInput) error {
	f.inputs = append(f.inputs, in)
	return nil
}

type fakeClaims struct {
	seen map[string]bool
	err  error
}

func (f *fakeClaims) Claim(_ context.Context, key, _, _ string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

type fakeBilling struct {
	short           bool
	err             error
	charges         []string
	refunds         []string
	order           *[]string
	attachmentBytes []int64
}

func (f *fakeBilling) ChargeMessageWithAttachments(ctx context.Context, workspaceID, key string, encodedAttachmentBytes int64) (balance_usecase.EmailQuote, error) {
	q, err := f.ChargeMessage(ctx, workspaceID, key)
	if err == nil {
		f.attachmentBytes = append(f.attachmentBytes, encodedAttachmentBytes)
	}
	return q, err
}

func (f *fakeBilling) ChargeMessage(_ context.Context, workspaceID, key string) (balance_usecase.EmailQuote, error) {
	if f.order != nil {
		*f.order = append(*f.order, "charge")
	}
	if f.short {
		return balance_usecase.EmailQuote{}, &balance_usecase.InsufficientEmailBalanceError{MissingMicros: 10}
	}
	if f.err != nil {
		return balance_usecase.EmailQuote{}, f.err
	}
	f.charges = append(f.charges, workspaceID+"|"+key)
	return balance_usecase.EmailQuote{Units: 1, UnitMicros: 100, TotalMicros: 100}, nil
}

func (f *fakeBilling) RefundMessage(_ context.Context, workspaceID, key string, charge balance_usecase.EmailQuote) error {
	if charge.Units != 1 {
		return errors.New("refund without the charge quote")
	}
	f.refunds = append(f.refunds, workspaceID+"|"+key)
	return nil
}

type fakeGateway struct {
	requests []emailsending.SendRequest
	contents []emailsending.Content
	err      error
	order    *[]string
}

func (f *fakeGateway) Send(_ context.Context, req emailsending.SendRequest, compose emailsending.ComposeFunc) (*emailsending.Message, error) {
	if f.order != nil {
		*f.order = append(*f.order, "send")
	}
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	content, err := compose(emailsending.Envelope{MessageID: "msg-1"})
	if err != nil {
		return nil, err
	}
	f.contents = append(f.contents, content)
	return &emailsending.Message{ID: uuid.NewString(), ProviderMessageID: "ses-" + req.RunID, RunKind: req.RunKind, RunID: req.RunID}, nil
}

type fakeEvents struct {
	events []*conv_event.ConversationEvent
}

func (f *fakeEvents) Log(e *conv_event.ConversationEvent) { f.events = append(f.events, e) }

type fakeAccounts struct {
	byWorkspace map[string]*emailsending.SenderAccount
}

func (f *fakeAccounts) GetByWorkspace(_ context.Context, workspaceID string) (*emailsending.SenderAccount, error) {
	if a, ok := f.byWorkspace[workspaceID]; ok {
		return a, nil
	}
	return nil, emailsending.ErrAccountNotFound
}

type fakeSuppressions struct {
	suppressed map[string]bool
}

func (f *fakeSuppressions) Suppressed(_ context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]emailsending.SuppressReason, error) {
	out := map[emailaddress.Address]emailsending.SuppressReason{}
	for _, a := range addresses {
		if f.suppressed[workspaceID+"|"+string(a)] {
			out[a] = emailsending.SuppressHardBounce
		}
	}
	return out, nil
}

type fakeRefs struct {
	err error
}

func (f fakeRefs) OwnedBy(context.Context, string, automationrefs.Refs) error { return f.err }

type fakeAnnouncer struct {
	updated []string
}

func (f *fakeAnnouncer) BroadcastMessageUpdated(entryID, _ string, m *conversation.Message) {
	f.updated = append(f.updated, entryID+"|"+string(m.DeliveryStatus))
}

func (f *fakeAnnouncer) BroadcastEntryUpdate(string, string, *conversation.Message) {}

func mustCodec() *emailtoken.Codec {
	codec, err := emailtoken.NewCodec(testSecret)
	if err != nil {
		panic(err)
	}
	return codec
}

type fakeOriginals struct {
	byMessage map[string]em.SentOriginal
	err       error
	calls     int
}

func (f *fakeOriginals) Original(_ context.Context, sent emailsending.Message) (em.SentOriginal, bool, error) {
	f.calls++
	if f.err != nil {
		return em.SentOriginal{}, false, f.err
	}
	original, ok := f.byMessage[sent.ID]
	return original, ok, nil
}
