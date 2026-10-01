package facebook

import (
	"context"
	"sync"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

type fakeOAuth struct {
	grant    *fbdomain.TokenGrant
	debug    *fbdomain.TokenDebug
	identity *fbdomain.GrantIdentity
	pages    []*fbdomain.RemotePage
	err      error
}

func (f *fakeOAuth) BuildAuthorizeURL(state string) string { return "https://dialog?state=" + state }
func (f *fakeOAuth) ExchangeCode(context.Context, string) (*fbdomain.TokenGrant, error) {
	return f.grant, f.err
}
func (f *fakeOAuth) DebugToken(context.Context, string) (*fbdomain.TokenDebug, error) {
	return f.debug, nil
}
func (f *fakeOAuth) Identify(context.Context, string) (*fbdomain.GrantIdentity, error) {
	return f.identity, nil
}
func (f *fakeOAuth) ListPages(context.Context, string) ([]*fbdomain.RemotePage, error) {
	return f.pages, nil
}
func (f *fakeOAuth) GetPage(_ context.Context, _, fbPageID string) (*fbdomain.RemotePage, error) {
	for _, p := range f.pages {
		if p.FBPageID == fbPageID {
			return p, nil
		}
	}
	return nil, fbdomain.ErrPageNotFound
}

type fakeSubscription struct {
	err          error
	subscribed   []string
	unsubscribed []string
	active       []string
}

func (f *fakeSubscription) Subscribe(_ context.Context, fbPageID, _ string, fields []string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.subscribed = append(f.subscribed, fbPageID)
	return fields, nil
}
func (f *fakeSubscription) ActiveFields(context.Context, string, string) ([]string, error) {
	return f.active, nil
}
func (f *fakeSubscription) Unsubscribe(_ context.Context, fbPageID, _ string) error {
	f.unsubscribed = append(f.unsubscribed, fbPageID)
	return f.err
}

type fakeGrants struct {
	mu      sync.Mutex
	byID    map[string]*fbdomain.Grant
	revoked []string
	erased  []string
	checked []string
}

func newFakeGrants() *fakeGrants { return &fakeGrants{byID: map[string]*fbdomain.Grant{}} }

func (f *fakeGrants) Upsert(_ context.Context, g *fbdomain.Grant) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g.ID == "" {
		g.ID = "grant-" + g.AppScopedUserID
	}
	stored := *g
	f.byID[g.ID] = &stored
	return nil
}
func (f *fakeGrants) FindByID(_ context.Context, id string) (*fbdomain.Grant, error) {
	if g, ok := f.byID[id]; ok {
		return g, nil
	}
	return nil, fbdomain.ErrGrantNotFound
}
func (f *fakeGrants) ListActive(context.Context, int) ([]*fbdomain.Grant, error) {
	var out []*fbdomain.Grant
	for _, g := range f.byID {
		if g.Status != fbdomain.GrantRevoked {
			out = append(out, g)
		}
	}
	return out, nil
}
func (f *fakeGrants) ListByAppScopedUser(_ context.Context, asid string) ([]*fbdomain.Grant, error) {
	var out []*fbdomain.Grant
	for _, g := range f.byID {
		if g.AppScopedUserID == asid {
			out = append(out, g)
		}
	}
	return out, nil
}
func (f *fakeGrants) MarkChecked(_ context.Context, id string, scopes []string, granular map[string][]string, _ time.Time) error {
	f.checked = append(f.checked, id)
	if g, ok := f.byID[id]; ok {
		g.Scopes, g.GranularScopes = scopes, granular
	}
	return nil
}
func (f *fakeGrants) Revoke(_ context.Context, id string, _ time.Time) error {
	f.revoked = append(f.revoked, id)
	if g, ok := f.byID[id]; ok {
		g.Status = fbdomain.GrantRevoked
	}
	return nil
}
func (f *fakeGrants) EraseToken(_ context.Context, id string) error {
	f.erased = append(f.erased, id)
	return nil
}
func (f *fakeGrants) CountActivePages(_ context.Context, grantID string) (int64, error) {
	return 0, nil
}

type fakePages struct {
	mu       sync.Mutex
	byID     map[string]*fbdomain.Page
	deleted  map[string]bool
	statuses map[string]fbdomain.Status
	restored []string
	seq      int
}

func newFakePages(existing ...*fbdomain.Page) *fakePages {
	f := &fakePages{byID: map[string]*fbdomain.Page{}, deleted: map[string]bool{}, statuses: map[string]fbdomain.Status{}}
	for _, p := range existing {
		f.byID[p.ID] = p
	}
	return f
}

func (f *fakePages) Create(_ context.Context, p *fbdomain.Page) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.FBPageID == p.FBPageID {
			return fbdomain.ErrPageAlreadyLinked
		}
	}
	f.seq++
	p.ID = "page-" + p.FBPageID
	stored := *p
	f.byID[p.ID] = &stored
	return nil
}
func (f *fakePages) Update(_ context.Context, p *fbdomain.Page) error {
	stored := *p
	if previous, ok := f.byID[p.ID]; ok {
		stored.DepartmentID, stored.AgentID, stored.WorkflowID, stored.PipelineID = previous.DepartmentID, previous.AgentID, previous.WorkflowID, previous.PipelineID
		stored.EnableAgentResponses, stored.EnableWorkflow, stored.EnableAnalysis = previous.EnableAgentResponses, previous.EnableWorkflow, previous.EnableAnalysis
		stored.EnableAutoStaging, stored.EnableAutoMemory, stored.AutomationDisclosure = previous.EnableAutoStaging, previous.EnableAutoMemory, previous.AutomationDisclosure
	}
	f.byID[p.ID] = &stored
	return nil
}
func (f *fakePages) UpdateConfig(_ context.Context, p *fbdomain.Page) error {
	stored := *f.byID[p.ID]
	stored.AgentID, stored.WorkflowID, stored.PipelineID, stored.DepartmentID = p.AgentID, p.WorkflowID, p.PipelineID, p.DepartmentID
	stored.EnableAgentResponses, stored.EnableWorkflow, stored.EnableAnalysis = p.EnableAgentResponses, p.EnableWorkflow, p.EnableAnalysis
	stored.EnableAutoStaging, stored.EnableAutoMemory, stored.AutomationDisclosure = p.EnableAutoStaging, p.EnableAutoMemory, p.AutomationDisclosure
	f.byID[p.ID] = &stored
	return nil
}
func (f *fakePages) UpdateStatus(_ context.Context, id string, status fbdomain.Status, reason string) error {
	f.statuses[id] = status
	if p, ok := f.byID[id]; ok {
		p.Status, p.StatusReason = status, reason
	}
	return nil
}
func (f *fakePages) UpdateToken(_ context.Context, id, grantID, token string, scopes []string, tasks []fbdomain.Task) error {
	p := f.byID[id]
	p.GrantID, p.PageToken, p.GrantedScopes, p.Tasks = grantID, token, scopes, tasks
	return nil
}
func (f *fakePages) UpdateSubscription(_ context.Context, id string, fields []string, at time.Time) error {
	p := f.byID[id]
	p.SubscribedFields, p.WebhookSubscribedAt = fields, &at
	return nil
}
func (f *fakePages) UpdateRouting(_ context.Context, id string, isDefault *bool, at time.Time) error {
	p := f.byID[id]
	p.IsDefaultRouteApp, p.RoutingCheckedAt = isDefault, &at
	return nil
}
func (f *fakePages) UpdatePolicy(_ context.Context, id, action, reason string, at time.Time) error {
	p := f.byID[id]
	p.PolicyAction, p.PolicyReason, p.PolicyAt = action, reason, &at
	return nil
}
func (f *fakePages) UpdateProfile(_ context.Context, id string, remote *fbdomain.RemotePage, key string, at time.Time) error {
	p := f.byID[id]
	p.Name, p.HealthCheckedAt = remote.Name, &at
	if key != "" {
		p.PictureStorageKey = key
	}
	return nil
}
func (f *fakePages) FindByID(_ context.Context, id string) (*fbdomain.Page, error) {
	if p, ok := f.byID[id]; ok && !f.deleted[id] {
		copied := *p
		return &copied, nil
	}
	return nil, fbdomain.ErrPageNotFound
}
func (f *fakePages) FindByFBPageID(_ context.Context, fbPageID string) (*fbdomain.Page, error) {
	for id, p := range f.byID {
		if p.FBPageID == fbPageID && !f.deleted[id] {
			copied := *p
			return &copied, nil
		}
	}
	return nil, fbdomain.ErrPageNotFound
}
func (f *fakePages) FindByFBPageIDUnscoped(_ context.Context, fbPageID string) (*fbdomain.Page, error) {
	for _, p := range f.byID {
		if p.FBPageID == fbPageID {
			copied := *p
			return &copied, nil
		}
	}
	return nil, fbdomain.ErrPageNotFound
}
func (f *fakePages) ListByWorkspace(_ context.Context, in fbdomain.ListPagesInput) (*shared.PaginatedResult[*fbdomain.Page], error) {
	var items []*fbdomain.Page
	for _, p := range f.byID {
		if p.WorkspaceID == in.WorkspaceID {
			items = append(items, p)
		}
	}
	return &shared.PaginatedResult[*fbdomain.Page]{Items: items}, nil
}
func (f *fakePages) ListByGrant(_ context.Context, grantID string) ([]*fbdomain.Page, error) {
	var out []*fbdomain.Page
	for id, p := range f.byID {
		if p.GrantID == grantID && !f.deleted[id] {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakePages) ListConnected(context.Context, int, int) ([]*fbdomain.Page, error) {
	var out []*fbdomain.Page
	for id, p := range f.byID {
		if p.Status == fbdomain.StatusConnected && !f.deleted[id] {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakePages) Restore(_ context.Context, id string) error {
	f.restored = append(f.restored, id)
	delete(f.deleted, id)
	return nil
}
func (f *fakePages) Delete(_ context.Context, id string) error {
	f.deleted[id] = true
	return nil
}

type fakePictures struct {
	stored []string
	err    error
}

func (f *fakePictures) StorePagePicture(_ context.Context, pageID, url string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.stored = append(f.stored, pageID)
	return "pictures/" + pageID, nil
}

type fakeContacts struct {
	mu       sync.Mutex
	byID     map[string]*fbdomain.Contact
	profiles map[string]fbdomain.ContactProfile
}

func newFakeContacts() *fakeContacts {
	return &fakeContacts{byID: map[string]*fbdomain.Contact{}, profiles: map[string]fbdomain.ContactProfile{}}
}

func (f *fakeContacts) FindOrCreate(_ context.Context, workspaceID, pageID, psid string) (*fbdomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.PageID == pageID && c.PSID == psid {
			return c, nil
		}
	}
	c := &fbdomain.Contact{ID: "contact-" + psid, WorkspaceID: workspaceID, PageID: pageID, PSID: psid, ProfileStatus: fbdomain.ProfileUnknown}
	f.byID[c.ID] = c
	return c, nil
}
func (f *fakeContacts) FindByID(_ context.Context, id string) (*fbdomain.Contact, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, fbdomain.ErrContactNotFound
}
func (f *fakeContacts) FindByIDs(_ context.Context, ids []string) ([]*fbdomain.Contact, error) {
	var out []*fbdomain.Contact
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeContacts) FindByPSID(_ context.Context, pageID, psid string) (*fbdomain.Contact, error) {
	for _, c := range f.byID {
		if c.PageID == pageID && c.PSID == psid {
			return c, nil
		}
	}
	return nil, fbdomain.ErrContactNotFound
}
func (f *fakeContacts) UpdateProfile(_ context.Context, id string, p fbdomain.ContactProfile) error {
	f.profiles[id] = p
	c := f.byID[id]
	c.ProfileStatus, c.ProfileFetchedAt = p.Status, &p.FetchedAt
	if p.Status == fbdomain.ProfileAvailable {
		c.Name, c.AvatarStorageKey = p.Name, p.AvatarStorageKey
	}
	return nil
}
func (f *fakeContacts) MarkUnreachable(_ context.Context, id, reason string) error {
	f.byID[id].Unreachable, f.byID[id].UnreachableReason = true, reason
	return nil
}
func (f *fakeContacts) SetBlocked(_ context.Context, id string, blocked bool) error {
	f.byID[id].Blocked = blocked
	return nil
}

type fakeConversations struct {
	mu         sync.Mutex
	byID       map[string]*fbdomain.Conversation
	inbound    []string
	outbound   []string
	watermarks map[string]time.Time
	metadata   map[string]map[string]any
}

func newFakeConversations() *fakeConversations {
	return &fakeConversations{byID: map[string]*fbdomain.Conversation{}, watermarks: map[string]time.Time{}, metadata: map[string]map[string]any{}}
}

func (f *fakeConversations) FindOrCreate(_ context.Context, workspaceID, pageID, contactID string) (*fbdomain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.PageID == pageID && c.ContactID == contactID {
			return c, nil
		}
	}
	c := &fbdomain.Conversation{ID: "conv-" + contactID, WorkspaceID: workspaceID, PageID: pageID, ContactID: contactID}
	f.byID[c.ID] = c
	return c, nil
}
func (f *fakeConversations) FindByID(_ context.Context, id string) (*fbdomain.Conversation, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, fbdomain.ErrConversationNotFound
}
func (f *fakeConversations) FindByContact(_ context.Context, pageID, contactID string) (*fbdomain.Conversation, error) {
	for _, c := range f.byID {
		if c.PageID == pageID && c.ContactID == contactID {
			return c, nil
		}
	}
	return nil, fbdomain.ErrConversationNotFound
}
func (f *fakeConversations) LatestForPage(_ context.Context, pageID string) (*fbdomain.Conversation, error) {
	for _, c := range f.byID {
		if c.PageID == pageID {
			return c, nil
		}
	}
	return nil, fbdomain.ErrConversationNotFound
}
func (f *fakeConversations) WorkspaceIDForEntry(_ context.Context, id string) (string, error) {
	if c, ok := f.byID[id]; ok {
		return c.WorkspaceID, nil
	}
	return "", fbdomain.ErrConversationNotFound
}
func (f *fakeConversations) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}
func (f *fakeConversations) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeConversations) RecordInbound(_ context.Context, id string, at time.Time) error {
	f.inbound = append(f.inbound, id)
	f.byID[id].LastCustomerMessageAt = &at
	return nil
}
func (f *fakeConversations) RecordOutbound(_ context.Context, id string, at time.Time) error {
	f.outbound = append(f.outbound, id)
	f.byID[id].LastAgentMessageAt = &at
	return nil
}
func (f *fakeConversations) AdvanceWatermark(_ context.Context, id string, kind fbdomain.WatermarkKind, at time.Time) error {
	f.watermarks[id+":"+string(kind)] = at
	return nil
}
func (f *fakeConversations) SetThreadOwner(_ context.Context, id, appID string, at time.Time) error {
	f.byID[id].ThreadOwnerAppID, f.byID[id].ThreadOwnerSeenAt = appID, &at
	return nil
}
func (f *fakeConversations) SetFBConversationID(context.Context, string, string) error { return nil }
func (f *fakeConversations) SetStatus(context.Context, string, conversation.StatusWrite) error {
	return nil
}
func (f *fakeConversations) SetAutomationEnabled(_ context.Context, id string, enabled *bool) error {
	f.byID[id].AutomationEnabled = enabled
	return nil
}
func (f *fakeConversations) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (f *fakeConversations) StatusForEntry(context.Context, string) (string, error) { return "", nil }

