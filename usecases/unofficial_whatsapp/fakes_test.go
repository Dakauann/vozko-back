package unofficial_whatsapp

import (
	"context"
	"errors"
	"sync"
	"time"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
)

// Hand-written fakes with XxxFn fields, matching the idiom the other channels'
// suites use: a test overrides only the behaviour it is about, and everything
// else keeps a sane default instead of nil-panicking.

// ---------------------------------------------------------------- servers

type fakeServerRepo struct {
	mu sync.Mutex

	servers map[string]*uw.Server
	// claims and releases record capacity accounting so a test can assert that
	// a failed provision gave its slot back.
	claims   int
	releases int

	ClaimFn func(ctx context.Context, serverID string) (bool, error)
	FindFn  func(ctx context.Context, id string) (*uw.Server, error)
	ListFn  func(ctx context.Context, workspaceID string) ([]*uw.Server, error)
}

func newFakeServerRepo(servers ...*uw.Server) *fakeServerRepo {
	byID := make(map[string]*uw.Server, len(servers))
	for _, s := range servers {
		byID[s.ID] = s
	}
	return &fakeServerRepo{servers: byID}
}

func (f *fakeServerRepo) Create(context.Context, *uw.Server) error { return nil }
func (f *fakeServerRepo) Update(context.Context, *uw.Server) error { return nil }

func (f *fakeServerRepo) FindByID(ctx context.Context, id string) (*uw.Server, error) {
	if f.FindFn != nil {
		return f.FindFn(ctx, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.servers[id]; ok {
		return s, nil
	}
	return nil, uw.ErrServerNotFound
}

func (f *fakeServerRepo) FindByBaseURL(context.Context, string) (*uw.Server, error) {
	return nil, uw.ErrServerNotFound
}

func (f *fakeServerRepo) ListPlacementCandidates(ctx context.Context, workspaceID string) ([]*uw.Server, error) {
	if f.ListFn != nil {
		return f.ListFn(ctx, workspaceID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*uw.Server, 0, len(f.servers))
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeServerRepo) ListAll(ctx context.Context) ([]*uw.Server, error) {
	return f.ListPlacementCandidates(ctx, "")
}

func (f *fakeServerRepo) ClaimCapacity(ctx context.Context, serverID string) (bool, error) {
	f.mu.Lock()
	f.claims++
	f.mu.Unlock()
	if f.ClaimFn != nil {
		return f.ClaimFn(ctx, serverID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	server, ok := f.servers[serverID]
	if !ok || !server.HasCapacity() {
		return false, nil
	}
	server.InUse++
	return true, nil
}

func (f *fakeServerRepo) ReleaseCapacity(_ context.Context, serverID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	if server, ok := f.servers[serverID]; ok && server.InUse > 0 {
		server.InUse--
	}
	return nil
}

func (f *fakeServerRepo) SyncCapacity(context.Context, string, int) error { return nil }
func (f *fakeServerRepo) RecordHealth(context.Context, string, *time.Time, string) error {
	return nil
}

// ---------------------------------------------------------------- instances

type fakeInstanceRepo struct {
	mu sync.Mutex

	instances map[string]*uw.Instance

	CreateFn             func(ctx context.Context, i *uw.Instance) error
	FindFn               func(ctx context.Context, id string) (*uw.Instance, error)
	UpdateSessionFn      func(ctx context.Context, id string, in uw.SessionUpdate) error
	ListForHealthCheckFn func(before time.Time) ([]*uw.Instance, error)
	ListConnectedFn      func() ([]*uw.Instance, error)

	// Recorded writes, so a test can assert what was persisted rather than
	// asserting on a returned value the caller could have fabricated.
	statusWrites  []uw.Status
	sessionWrites []uw.SessionUpdate
	deleted       []string
	webhookStamps int
}

func newFakeInstanceRepo(instances ...*uw.Instance) *fakeInstanceRepo {
	byID := make(map[string]*uw.Instance, len(instances))
	for _, i := range instances {
		byID[i.ID] = i
	}
	return &fakeInstanceRepo{instances: byID}
}

func (f *fakeInstanceRepo) Create(ctx context.Context, i *uw.Instance) error {
	if f.CreateFn != nil {
		return f.CreateFn(ctx, i)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i.ID == "" {
		i.ID = "inst-" + i.ProviderInstanceID
	}
	f.instances[i.ID] = i
	return nil
}

func (f *fakeInstanceRepo) Update(context.Context, *uw.Instance) error { return nil }

func (f *fakeInstanceRepo) UpdateStatus(_ context.Context, id string, status uw.Status, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusWrites = append(f.statusWrites, status)
	if inst, ok := f.instances[id]; ok {
		inst.Status = status
		inst.StatusReason = reason
	}
	return nil
}

func (f *fakeInstanceRepo) UpdateSession(ctx context.Context, id string, in uw.SessionUpdate) error {
	f.mu.Lock()
	f.sessionWrites = append(f.sessionWrites, in)
	f.mu.Unlock()
	if f.UpdateSessionFn != nil {
		return f.UpdateSessionFn(ctx, id, in)
	}
	return nil
}

func (f *fakeInstanceRepo) UpdateRestriction(context.Context, string, uw.Restriction) error {
	return nil
}

func (f *fakeInstanceRepo) SetWebhookRegistered(context.Context, string, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.webhookStamps++
	return nil
}

func (f *fakeInstanceRepo) RotateDeliveryToken(_ context.Context, id, token, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if inst, ok := f.instances[id]; ok {
		inst.DeliveryToken, inst.DeliveryTokenHash = token, hash
	}
	return nil
}

func (f *fakeInstanceRepo) FindByID(ctx context.Context, id string) (*uw.Instance, error) {
	if f.FindFn != nil {
		return f.FindFn(ctx, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if inst, ok := f.instances[id]; ok {
		return inst, nil
	}
	return nil, uw.ErrInstanceNotFound
}

func (f *fakeInstanceRepo) FindByDeliveryTokenHash(context.Context, string) (*uw.Instance, error) {
	return nil, uw.ErrInstanceNotFound
}
func (f *fakeInstanceRepo) FindByJID(context.Context, string) (*uw.Instance, error) {
	return nil, uw.ErrInstanceNotFound
}
func (f *fakeInstanceRepo) FindByProviderInstanceID(context.Context, string, string) (*uw.Instance, error) {
	return nil, uw.ErrInstanceNotFound
}

func (f *fakeInstanceRepo) ListByWorkspace(context.Context, uw.ListInstancesInput) (*shared.PaginatedResult[*uw.Instance], error) {
	return nil, nil
}
func (f *fakeInstanceRepo) ListByServer(context.Context, string) ([]*uw.Instance, error) {
	return nil, nil
}
func (f *fakeInstanceRepo) ListForHealthCheck(_ context.Context, before time.Time, _ int) ([]*uw.Instance, error) {
	if f.ListForHealthCheckFn != nil {
		return f.ListForHealthCheckFn(before)
	}
	return nil, nil
}

func (f *fakeInstanceRepo) ListConnected(context.Context, int) ([]*uw.Instance, error) {
	if f.ListConnectedFn != nil {
		return f.ListConnectedFn()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*uw.Instance, 0, len(f.instances))
	for _, inst := range f.instances {
		if inst.SessionLive() {
			out = append(out, inst)
		}
	}
	return out, nil
}
func (f *fakeInstanceRepo) CountByServer(context.Context, string) (int, error) { return 0, nil }

func (f *fakeInstanceRepo) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	delete(f.instances, id)
	return nil
}

// ---------------------------------------------------------------- provider

type fakeProvider struct {
	mu sync.Mutex

	CreateInstanceFn func(ctx context.Context, s uw.ServerRef, in uw.CreateInstanceInput) (*uw.CreatedInstance, error)
	ConnectFn        func(ctx context.Context, ref uw.InstanceRef, in uw.ConnectInput) (*uw.Session, error)
	StatusFn         func(ctx context.Context, ref uw.InstanceRef) (*uw.Session, error)
	SetWebhookFn     func(ctx context.Context, ref uw.InstanceRef, sub uw.WebhookSubscription) error
	GetWebhooksFn    func(ctx context.Context, ref uw.InstanceRef) ([]uw.WebhookSubscription, error)
	DisconnectFn     func(ctx context.Context, ref uw.InstanceRef) error
	DeleteInstanceFn func(ctx context.Context, ref uw.InstanceRef) error

	// Call records, so a test can assert that a compensating action ran.
	created         []uw.CreateInstanceInput
	deletedTokens   []string
	webhookSets     []uw.WebhookSubscription
	chatbotDisabled int
}

func (f *fakeProvider) CreateInstance(ctx context.Context, s uw.ServerRef, in uw.CreateInstanceInput) (*uw.CreatedInstance, error) {
	f.mu.Lock()
	f.created = append(f.created, in)
	f.mu.Unlock()
	if f.CreateInstanceFn != nil {
		return f.CreateInstanceFn(ctx, s, in)
	}
	return &uw.CreatedInstance{ProviderInstanceID: "r18", Token: "instance-token", Name: in.Name}, nil
}

func (f *fakeProvider) ListInstances(context.Context, uw.ServerRef) ([]uw.RemoteInstance, error) {
	return nil, nil
}

func (f *fakeProvider) Connect(ctx context.Context, ref uw.InstanceRef, in uw.ConnectInput) (*uw.Session, error) {
	if f.ConnectFn != nil {
		return f.ConnectFn(ctx, ref, in)
	}
	return &uw.Session{State: "connecting", QRCode: "data:image/png;base64,AAA"}, nil
}

func (f *fakeProvider) Status(ctx context.Context, ref uw.InstanceRef) (*uw.Session, error) {
	if f.StatusFn != nil {
		return f.StatusFn(ctx, ref)
	}
	return &uw.Session{State: "connected", Connected: true, LoggedIn: true,
		JID: "5511999999999@s.whatsapp.net"}, nil
}

func (f *fakeProvider) Disconnect(ctx context.Context, ref uw.InstanceRef) error {
	if f.DisconnectFn != nil {
		return f.DisconnectFn(ctx, ref)
	}
	return nil
}

func (f *fakeProvider) Reset(context.Context, uw.InstanceRef) error { return nil }

func (f *fakeProvider) DeleteInstance(ctx context.Context, ref uw.InstanceRef) error {
	f.mu.Lock()
	f.deletedTokens = append(f.deletedTokens, ref.Token)
	f.mu.Unlock()
	if f.DeleteInstanceFn != nil {
		return f.DeleteInstanceFn(ctx, ref)
	}
	return nil
}

func (f *fakeProvider) SetWebhook(ctx context.Context, ref uw.InstanceRef, sub uw.WebhookSubscription) error {
	f.mu.Lock()
	f.webhookSets = append(f.webhookSets, sub)
	f.mu.Unlock()
	if f.SetWebhookFn != nil {
		return f.SetWebhookFn(ctx, ref, sub)
	}
	return nil
}

func (f *fakeProvider) GetWebhooks(ctx context.Context, ref uw.InstanceRef) ([]uw.WebhookSubscription, error) {
	if f.GetWebhooksFn != nil {
		return f.GetWebhooksFn(ctx, ref)
	}
	return nil, nil
}

func (f *fakeProvider) WebhookErrors(context.Context, uw.InstanceRef) ([]uw.WebhookDeliveryError, error) {
	return nil, nil
}

func (f *fakeProvider) MessagingLimits(context.Context, uw.InstanceRef) (*uw.Restriction, error) {
	return &uw.Restriction{}, nil
}

func (f *fakeProvider) DisableBuiltInChatbot(context.Context, uw.InstanceRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatbotDisabled++
	return nil
}

var _ uw.ProviderAPI = (*fakeProvider)(nil)

// errBoom is a generic transport failure for tests that only care that
// something went wrong, not what.
var errBoom = errors.New("boom")

func healthyServer(id string, capacity, inUse int) *uw.Server {
	return &uw.Server{
		ID: id, Name: id, BaseURL: "https://" + id + ".example.com",
		AdminToken: "admin", Provider: uw.ProviderUazapi,
		Capacity: capacity, InUse: inUse, Enabled: true,
	}
}

// ---------------------------------------------------------------- messaging

type fakeMessaging struct {
	mu sync.Mutex

	SendTextFn     func(ctx context.Context, ref uw.InstanceRef, in uw.SendTextInput) (*uw.SendResult, error)
	SendMenuFn     func(ctx context.Context, ref uw.InstanceRef, in uw.SendMenuInput) (*uw.SendResult, error)
	CheckNumbersFn func(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error)

	texts    []uw.SendTextInput
	media    []uw.SendMediaInput
	menus    []uw.SendMenuInput
	presence []uw.Presence
	reacts   []string
	edits    []string
	deletes  []string
	reads    [][]string
}

func (f *fakeMessaging) SendText(ctx context.Context, ref uw.InstanceRef, in uw.SendTextInput) (*uw.SendResult, error) {
	f.mu.Lock()
	f.texts = append(f.texts, in)
	f.mu.Unlock()
	if f.SendTextFn != nil {
		return f.SendTextFn(ctx, ref, in)
	}
	return &uw.SendResult{ProviderMessageID: "pm-1", Status: uw.DeliverySent}, nil
}

func (f *fakeMessaging) SendMedia(_ context.Context, _ uw.InstanceRef, in uw.SendMediaInput) (*uw.SendResult, error) {
	f.mu.Lock()
	f.media = append(f.media, in)
	f.mu.Unlock()
	return &uw.SendResult{ProviderMessageID: "pm-media"}, nil
}

func (f *fakeMessaging) SendMenu(ctx context.Context, ref uw.InstanceRef, in uw.SendMenuInput) (*uw.SendResult, error) {
	f.mu.Lock()
	f.menus = append(f.menus, in)
	f.mu.Unlock()
	if f.SendMenuFn != nil {
		return f.SendMenuFn(ctx, ref, in)
	}
	return &uw.SendResult{ProviderMessageID: "pm-menu"}, nil
}

func (f *fakeMessaging) SendPresence(_ context.Context, _ uw.InstanceRef, _ string, p uw.Presence, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presence = append(f.presence, p)
	return nil
}

func (f *fakeMessaging) MarkRead(_ context.Context, _ uw.InstanceRef, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, ids)
	return nil
}

func (f *fakeMessaging) React(_ context.Context, _ uw.InstanceRef, _, id, emoji string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reacts = append(f.reacts, id+":"+emoji)
	return nil
}

func (f *fakeMessaging) EditMessage(_ context.Context, _ uw.InstanceRef, id, text string) (*uw.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, id+":"+text)
	return &uw.SendResult{ProviderMessageID: id}, nil
}

func (f *fakeMessaging) DeleteMessage(_ context.Context, _ uw.InstanceRef, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes = append(f.deletes, id)
	return nil
}

func (f *fakeMessaging) DownloadMedia(context.Context, uw.InstanceRef, string) (*uw.RemoteMedia, error) {
	return &uw.RemoteMedia{Data: []byte("bytes"), MIMEType: "image/jpeg"}, nil
}

func (f *fakeMessaging) CheckNumbers(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error) {
	if f.CheckNumbersFn != nil {
		return f.CheckNumbersFn(ctx, ref, numbers)
	}
	out := make([]uw.NumberCheck, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, uw.NumberCheck{Query: n, JID: uw.UserJID(n), IsOnWhatsApp: true})
	}
	return out, nil
}

var _ uw.MessagingAPI = (*fakeMessaging)(nil)

// ---------------------------------------------------------------- contacts

type fakeContactRepo struct {
	mu       sync.Mutex
	contacts map[string]*uw.Contact
	created  []*uw.Contact
}

func newFakeContactRepo(contacts ...*uw.Contact) *fakeContactRepo {
	byID := make(map[string]*uw.Contact, len(contacts))
	for _, c := range contacts {
		byID[c.ID] = c
	}
	return &fakeContactRepo{contacts: byID}
}

func (f *fakeContactRepo) FindOrCreate(_ context.Context, in uw.FindOrCreateContactInput) (*uw.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.contacts {
		if c.JID == in.JID && c.InstanceID == in.InstanceID {
			return c, nil
		}
	}
	contact := &uw.Contact{
		ID: "contact-" + in.JID, WorkspaceID: in.WorkspaceID, InstanceID: in.InstanceID,
		JID: in.JID, LID: in.LID, PhoneNumber: in.PhoneNumber, Name: in.Name,
	}
	f.contacts[contact.ID] = contact
	f.created = append(f.created, contact)
	return contact, nil
}

func (f *fakeContactRepo) FindByID(_ context.Context, id string) (*uw.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.contacts[id]; ok {
		return c, nil
	}
	return nil, uw.ErrContactNotFound
}

func (f *fakeContactRepo) FindByIDs(context.Context, []string) ([]*uw.Contact, error) {
	return nil, nil
}

func (f *fakeContactRepo) FindByJID(_ context.Context, instanceID, jid string) (*uw.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.contacts {
		if c.InstanceID == instanceID && c.JID == jid {
			return c, nil
		}
	}
	return nil, uw.ErrContactNotFound
}

func (f *fakeContactRepo) UpdateProfile(context.Context, string, uw.ContactProfile) error { return nil }

func (f *fakeContactRepo) SetBlocked(_ context.Context, id string, blocked bool, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.contacts[id]; ok {
		c.Blocked = blocked
	}
	return nil
}

func (f *fakeContactRepo) LinkLead(_ context.Context, id, leadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.contacts[id]; ok && c.LeadID == nil {
		c.LeadID = &leadID
	}
	return nil
}

// ---------------------------------------------------------------- conversations

type fakeConversationRepo struct {
	mu      sync.Mutex
	convs   map[string]*uw.Conversation
	created []*uw.Conversation
}

func newFakeConversationRepo(convs ...*uw.Conversation) *fakeConversationRepo {
	byID := make(map[string]*uw.Conversation, len(convs))
	for _, c := range convs {
		byID[c.ID] = c
	}
	return &fakeConversationRepo{convs: byID}
}

func (f *fakeConversationRepo) FindOrCreate(_ context.Context, in uw.FindOrCreateConversationInput) (*uw.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.convs {
		if c.InstanceID == in.InstanceID && c.ContactID == in.ContactID {
			return c, nil
		}
	}
	conv := &uw.Conversation{
		ID: "conv-" + in.ContactID, WorkspaceID: in.WorkspaceID,
		InstanceID: in.InstanceID, ContactID: in.ContactID,
		ChatID: in.ChatID, IsGroup: in.IsGroup,
	}
	f.convs[conv.ID] = conv
	f.created = append(f.created, conv)
	return conv, nil
}

func (f *fakeConversationRepo) FindByID(_ context.Context, id string) (*uw.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.convs[id]; ok {
		return c, nil
	}
	return nil, uw.ErrConversationNotFound
}

func (f *fakeConversationRepo) FindByChatID(_ context.Context, instanceID, chatID string) (*uw.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.convs {
		if c.InstanceID == instanceID && c.ChatID == chatID {
			return c, nil
		}
	}
	return nil, uw.ErrConversationNotFound
}

func (f *fakeConversationRepo) WorkspaceIDForEntry(context.Context, string) (string, error) {
	return "ws-1", nil
}
func (f *fakeConversationRepo) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeConversationRepo) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeConversationRepo) RecordInbound(context.Context, string, time.Time) error  { return nil }
func (f *fakeConversationRepo) RecordOutbound(context.Context, string, time.Time) error { return nil }
func (f *fakeConversationRepo) SetStatus(context.Context, string, string, string, string, *time.Time) error {
	return nil
}
func (f *fakeConversationRepo) SetAutomationEnabled(context.Context, string, *bool) error { return nil }
func (f *fakeConversationRepo) StatusForEntry(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeConversationRepo) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}
