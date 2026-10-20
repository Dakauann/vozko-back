package tiktok

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	conversation_usecase "vozko/usecases/conversation"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return testNow }

type fakeAccounts struct {
	mu       sync.Mutex
	byID     map[string]*ttdomain.Account
	deleted  map[string]bool
	lockErr  error
	refreshN int
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{byID: map[string]*ttdomain.Account{}, deleted: map[string]bool{}}
}

func cloneAccount(a *ttdomain.Account) *ttdomain.Account {
	c := *a
	c.GrantedScopes = append([]string(nil), a.GrantedScopes...)
	return &c
}

func (f *fakeAccounts) put(a *ttdomain.Account) *ttdomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	f.byID[a.ID] = cloneAccount(a)
	return a
}

func (f *fakeAccounts) get(id string) *ttdomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		return cloneAccount(a)
	}
	return nil
}

func (f *fakeAccounts) Create(ctx context.Context, a *ttdomain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	for id, existing := range f.byID {
		if existing.OpenID == a.OpenID && !f.deleted[id] {
			f.mu.Unlock()
			return ttdomain.ErrAccountAlreadyLinked
		}
	}
	f.mu.Unlock()
	f.put(a)
	return nil
}

func (f *fakeAccounts) Update(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[a.ID]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	cur.DepartmentID, cur.AgentID, cur.WorkflowID, cur.PipelineID = a.DepartmentID, a.AgentID, a.WorkflowID, a.PipelineID
	cur.EnableAgentResponses, cur.EnableWorkflow, cur.EnableAnalysis = a.EnableAgentResponses, a.EnableWorkflow, a.EnableAnalysis
	cur.EnableAutoStaging, cur.EnableAutoMemory, cur.AutomationDisclosure = a.EnableAutoStaging, a.EnableAutoMemory, a.AutomationDisclosure
	return nil
}

func (f *fakeAccounts) Reconnect(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.byID[a.ID]
	if !ok || cur.WorkspaceID != a.WorkspaceID {
		return ttdomain.ErrAccountNotFound
	}
	updated := cloneAccount(a)
	updated.CreatedAt = cur.CreatedAt
	updated.PurgeAfter = nil
	f.byID[a.ID] = updated
	delete(f.deleted, a.ID)
	return nil
}

func (f *fakeAccounts) UpdateStatus(ctx context.Context, id string, status ttdomain.Status, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	a.Status, a.StatusReason = status, reason
	if status != ttdomain.StatusConnected {
		a.AccessToken, a.RefreshToken, a.AccessExpiresAt = "", "", nil
	}
	return nil
}

func (f *fakeAccounts) UpdateProfile(ctx context.Context, id string, p ttdomain.ProfileUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	a.Username, a.DisplayName, a.AvatarKey = p.Username, p.DisplayName, p.AvatarKey
	a.HasAdvancedAccess, a.IsBusinessAccount, a.GrantedScopes, a.Capabilities = p.HasAdvancedAccess, p.IsBusinessAccount, p.GrantedScopes, p.Capabilities
	return nil
}

func (f *fakeAccounts) LearnUniqueIdentifier(ctx context.Context, id, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok && a.UniqueIdentifier == "" {
		a.UniqueIdentifier = uid
	}
	return nil
}

func (f *fakeAccounts) SetWebhooksHealthy(ctx context.Context, id string, healthy bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	a.WebhooksHealthy = healthy
	return nil
}

func (f *fakeAccounts) FindByID(ctx context.Context, id string) (*ttdomain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok || f.deleted[id] {
		return nil, ttdomain.ErrAccountNotFound
	}
	return cloneAccount(a), nil
}

func (f *fakeAccounts) FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*ttdomain.Account, error) {
	a, err := f.FindByID(ctx, id)
	if err != nil || a.WorkspaceID != workspaceID {
		return nil, ttdomain.ErrAccountNotFound
	}
	return a, nil
}

func (f *fakeAccounts) FindLiveByOpenID(ctx context.Context, openID string) (*ttdomain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, a := range f.byID {
		if a.OpenID == openID && !f.deleted[id] {
			return cloneAccount(a), nil
		}
	}
	return nil, ttdomain.ErrAccountNotFound
}

func (f *fakeAccounts) FindByWorkspaceAndOpenIDUnscoped(ctx context.Context, workspaceID, openID string) (*ttdomain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.byID {
		if a.OpenID == openID && a.WorkspaceID == workspaceID {
			return cloneAccount(a), nil
		}
	}
	return nil, ttdomain.ErrAccountNotFound
}

func (f *fakeAccounts) Restore(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.deleted, id)
	if a, ok := f.byID[id]; ok {
		a.PurgeAfter = nil
	}
	return nil
}

func (f *fakeAccounts) sorted(match func(*ttdomain.Account) bool) []*ttdomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ttdomain.Account
	for id, a := range f.byID {
		if match(a) && !f.deleted[id] {
			out = append(out, cloneAccount(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (f *fakeAccounts) ListByWorkspace(ctx context.Context, in ttdomain.ListAccountsInput) (*shared.PaginatedResult[*ttdomain.Account], error) {
	items := f.sorted(func(a *ttdomain.Account) bool { return a.WorkspaceID == in.WorkspaceID })
	return &shared.PaginatedResult[*ttdomain.Account]{Items: items, Page: 1, PageSize: len(items), TotalItems: int64(len(items)), TotalPages: 1}, nil
}

func (f *fakeAccounts) ListConnected(ctx context.Context, afterID string, limit int) ([]*ttdomain.Account, error) {
	var out []*ttdomain.Account
	for _, a := range f.sorted(func(a *ttdomain.Account) bool { return a.Status == ttdomain.StatusConnected }) {
		if a.ID > afterID && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeAccounts) ListDueForRefresh(ctx context.Context, before time.Time, limit int) ([]*ttdomain.Account, error) {
	return f.sorted(func(a *ttdomain.Account) bool {
		return a.Status == ttdomain.StatusConnected && a.RefreshToken != "" && (a.AccessExpiresAt == nil || a.AccessExpiresAt.Before(before))
	}), nil
}

func (f *fakeAccounts) LockForRefresh(ctx context.Context, id string, refresh ttdomain.RefreshFunc) (bool, error) {
	if f.lockErr != nil {
		return false, f.lockErr
	}
	a := f.get(id)
	if a == nil {
		return false, nil
	}
	f.mu.Lock()
	f.refreshN++
	f.mu.Unlock()
	outcome, err := refresh(ctx, a)
	if err != nil {
		return true, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.byID[id]
	if outcome.Status != "" {
		cur.Status, cur.StatusReason = outcome.Status, outcome.StatusReason
		cur.PurgeAfter = outcome.PurgeAfter
		if outcome.Status != ttdomain.StatusConnected && outcome.Tokens == nil {
			cur.AccessToken, cur.RefreshToken, cur.AccessExpiresAt = "", "", nil
		}
	}
	if t := outcome.Tokens; t != nil {
		cur.AccessToken, cur.RefreshToken = t.AccessToken, t.RefreshToken
		access, refreshAt := t.AccessExpiresAt, t.RefreshExpiresAt
		cur.AccessExpiresAt, cur.RefreshExpiresAt = &access, &refreshAt
		if len(t.GrantedScopes) > 0 {
			cur.GrantedScopes = t.GrantedScopes
		}
	}
	return true, nil
}

func (f *fakeAccounts) ListDueForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*ttdomain.Account, error) {
	return f.sorted(func(a *ttdomain.Account) bool {
		return (a.Status == ttdomain.StatusConnected || a.Status == ttdomain.StatusNeedsReconnect) && (a.LastHealthCheckAt == nil || a.LastHealthCheckAt.Before(before))
	}), nil
}

func (f *fakeAccounts) MarkHealthChecked(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.byID[id]; ok {
		a.LastHealthCheckAt = &at
	}
	return nil
}

func (f *fakeAccounts) Disconnect(ctx context.Context, id string, status ttdomain.Status, reason string, purgeAfter time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	a.Status, a.StatusReason, a.AccessToken, a.RefreshToken, a.PurgeAfter = status, reason, "", "", &purgeAfter
	f.deleted[id] = true
	return nil
}

func (f *fakeAccounts) Revoke(ctx context.Context, id string, reason string, purgeAfter time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrAccountNotFound
	}
	a.Status, a.StatusReason, a.AccessToken, a.RefreshToken, a.AccessExpiresAt, a.PurgeAfter = ttdomain.StatusRevoked, reason, "", "", nil, &purgeAfter
	return nil
}

func (f *fakeAccounts) ListDueForPurge(ctx context.Context, now time.Time, limit int) ([]*ttdomain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ttdomain.Account
	for _, a := range f.byID {
		if a.PurgeDue(now) {
			out = append(out, cloneAccount(a))
		}
	}
	return out, nil
}

func (f *fakeAccounts) DeletePurged(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	delete(f.deleted, id)
	return nil
}

type fakeContacts struct {
	mu   sync.Mutex
	byID map[string]*ttdomain.Contact
}

func newFakeContacts() *fakeContacts { return &fakeContacts{byID: map[string]*ttdomain.Contact{}} }

func (f *fakeContacts) FindOrCreate(ctx context.Context, in ttdomain.FindOrCreateContactInput) (*ttdomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.AccountID == in.AccountID && c.UniqueIdentifier == in.UniqueIdentifier {
			if in.Username != "" {
				c.Username = in.Username
			}
			cp := *c
			return &cp, nil
		}
	}
	c := &ttdomain.Contact{ID: uuid.NewString(), WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, UniqueIdentifier: in.UniqueIdentifier, Username: in.Username, IsFollower: in.IsFollower}
	f.byID[c.ID] = c
	cp := *c
	return &cp, nil
}

func (f *fakeContacts) FindByID(ctx context.Context, id string) (*ttdomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return nil, ttdomain.ErrContactNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeContacts) FindByIDs(ctx context.Context, ids []string) ([]*ttdomain.Contact, error) {
	var out []*ttdomain.Contact
	for _, id := range ids {
		if c, err := f.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeContacts) FindByUniqueIdentifier(ctx context.Context, accountID, uid string) (*ttdomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.AccountID == accountID && c.UniqueIdentifier == uid {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ttdomain.ErrContactNotFound
}

func (f *fakeContacts) UpdateProfile(ctx context.Context, id string, p ttdomain.ContactProfile, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrContactNotFound
	}
	if p.Username != "" {
		c.Username = p.Username
	}
	if p.DisplayName != "" {
		c.DisplayName = p.DisplayName
	}
	if p.AvatarKey != "" {
		c.AvatarKey = p.AvatarKey
	}
	c.ProfileRefreshedAt = &at
	return nil
}

func (f *fakeContacts) SetLeadID(ctx context.Context, id string, leadID *string) error { return nil }

type fakeConversations struct {
	mu        sync.Mutex
	byID      map[string]*ttdomain.Conversation
	statuses  map[string]conversation.StatusWrite
	outbound  map[string][]time.Time
	claimErr  error
	released  int
	closedAt  map[string]time.Time
	counted   int
	automated map[string]*bool
}

func newFakeConversations() *fakeConversations {
	return &fakeConversations{byID: map[string]*ttdomain.Conversation{}, statuses: map[string]conversation.StatusWrite{}, outbound: map[string][]time.Time{}, closedAt: map[string]time.Time{}, automated: map[string]*bool{}}
}

func (f *fakeConversations) FindOrCreate(ctx context.Context, in ttdomain.FindOrCreateConversationInput) (*ttdomain.Conversation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.AccountID == in.AccountID && c.ConversationID == in.ConversationID {
			cp := *c
			return &cp, false, nil
		}
	}
	c := &ttdomain.Conversation{ID: uuid.NewString(), WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, ContactID: in.ContactID, ConversationID: in.ConversationID}
	f.byID[c.ID] = c
	cp := *c
	return &cp, true, nil
}

func (f *fakeConversations) FindByID(ctx context.Context, id string) (*ttdomain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return nil, ttdomain.ErrConversationNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeConversations) FindByIDs(ctx context.Context, ids []string) ([]*ttdomain.Conversation, error) {
	var out []*ttdomain.Conversation
	for _, id := range ids {
		if c, err := f.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeConversations) FindByConversationID(ctx context.Context, accountID, conversationID string) (*ttdomain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if c.AccountID == accountID && c.ConversationID == conversationID {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ttdomain.ErrConversationNotFound
}

func (f *fakeConversations) WorkspaceIDForEntry(ctx context.Context, id string) (string, error) {
	c, err := f.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	return c.WorkspaceID, nil
}

func (f *fakeConversations) DepartmentIDForEntry(ctx context.Context, id string) (string, error) {
	return "", nil
}

func (f *fakeConversations) ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	return nil, nil
}

func (f *fakeConversations) ClaimOutbound(ctx context.Context, id string, now time.Time) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return false, nil
	}
	if !c.Window(now).Open {
		return false, nil
	}
	c.OutboundSinceInbound++
	return true, nil
}

func (f *fakeConversations) ReleaseOutbound(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released++
	if c, ok := f.byID[id]; ok && c.OutboundSinceInbound > 0 {
		c.OutboundSinceInbound--
	}
	return nil
}

func (f *fakeConversations) CountOutbound(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counted++
	if c, ok := f.byID[id]; ok {
		c.OutboundSinceInbound++
	}
	return nil
}

func (f *fakeConversations) CloseBudget(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedAt[id] = at
	if c, ok := f.byID[id]; ok {
		c.BudgetClosedAt = &at
	}
	return nil
}

func (f *fakeConversations) RecordInbound(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return ttdomain.ErrConversationNotFound
	}
	if c.LastCustomerMessageAt == nil || c.LastCustomerMessageAt.Before(at) {
		c.OutboundSinceInbound = 0
		c.LastCustomerMessageAt = &at
	}
	if c.LastMessageAt == nil || c.LastMessageAt.Before(at) {
		c.LastMessageAt = &at
	}
	return nil
}

func (f *fakeConversations) RecordOutbound(ctx context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outbound[id] = append(f.outbound[id], at)
	if c, ok := f.byID[id]; ok && (c.LastMessageAt == nil || c.LastMessageAt.Before(at)) {
		c.LastMessageAt = &at
	}
	return nil
}

func (f *fakeConversations) SetImageCapability(ctx context.Context, id string, allowed bool, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[id]; ok {
		c.ImageSendAllowed, c.ImageSendCheckedAt = &allowed, &at
	}
	return nil
}

func (f *fakeConversations) SetReferral(ctx context.Context, id string, referral ttdomain.Referral) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[id]; ok {
		c.Referral = &referral
	}
	return nil
}

func (f *fakeConversations) SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[id] = write
	return nil
}

func (f *fakeConversations) SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.automated[id] = enabled
	if c, ok := f.byID[id]; ok {
		c.AutomationEnabled = enabled
	}
	return nil
}

func (f *fakeConversations) StatusForEntry(ctx context.Context, id string) (string, error) {
	return "", nil
}

func (f *fakeConversations) CountByStatus(ctx context.Context, workspaceID, accountID string) (map[string]int64, error) {
	return map[string]int64{}, nil
}

type fakeServices struct {
	mu sync.Mutex

	grant      ttdomain.Grant
	grantErr   error
	refreshed  ttdomain.Grant
	refreshErr error
	info       ttdomain.TokenInfo
	infoErr    error
	revoked    []string

	profile    ttdomain.BusinessProfile
	profileErr error

	probeErr error

	sentTexts   []sentText
	sentCards   []ttdomain.Card
	sentImages  []string
	uploads     int
	actions     []ttdomain.SenderAction
	sendErr     error
	imageOK     bool
	imageErr    error
	download    ttdomain.DownloadedMedia
	downloadErr error
	people      []ttdomain.Participant
	peopleErr   error

	videos    ttdomain.VideoPage
	videosErr error

	commentPage ttdomain.CommentPage
	lookup      map[string]ttdomain.RemoteComment
	lookupErr   error
	lookupCalls [][]string
	replies     []sentReply
	replyErr    error
	hidden      map[string]bool
	liked       map[string]bool

	subscriptions map[ttdomain.WebhookKind]*ttdomain.WebhookSubscription
	subscribed    []ttdomain.WebhookKind
	subErr        error
}

type sentText struct {
	conversationID, text, replyTo string
}

type sentReply struct {
	videoID, commentID, text string
}

func newFakeServices() *fakeServices {
	return &fakeServices{lookup: map[string]ttdomain.RemoteComment{}, hidden: map[string]bool{}, liked: map[string]bool{}, subscriptions: map[ttdomain.WebhookKind]*ttdomain.WebhookSubscription{}}
}

func (f *fakeServices) services(errs ttdomain.ErrorClassifier) Services {
	return Services{OAuth: f, Profiles: f, Messaging: f, Videos: f, Comments: f, Webhooks: f, Errors: errs}
}

func (f *fakeServices) AuthorizeURL(state string) (string, error) {
	return "https://www.tiktok.com/v2/auth/authorize?state=" + state, nil
}

func (f *fakeServices) ExchangeCode(ctx context.Context, code string) (ttdomain.Grant, error) {
	return f.grant, f.grantErr
}

func (f *fakeServices) Refresh(ctx context.Context, refreshToken string) (ttdomain.Grant, error) {
	return f.refreshed, f.refreshErr
}

func (f *fakeServices) TokenInfo(ctx context.Context, accessToken string) (ttdomain.TokenInfo, error) {
	return f.info, f.infoErr
}

func (f *fakeServices) Revoke(ctx context.Context, accessToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, accessToken)
	return nil
}

func (f *fakeServices) Profile(ctx context.Context, creds ttdomain.Credentials) (ttdomain.BusinessProfile, error) {
	return f.profile, f.profileErr
}

func (f *fakeServices) SendText(ctx context.Context, creds ttdomain.Credentials, conversationID, text, replyTo string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return "", f.sendErr
	}
	f.sentTexts = append(f.sentTexts, sentText{conversationID, text, replyTo})
	return fmt.Sprintf("out-%d", len(f.sentTexts)), nil
}

func (f *fakeServices) SendImage(ctx context.Context, creds ttdomain.Credentials, conversationID, mediaID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return "", f.sendErr
	}
	f.sentImages = append(f.sentImages, mediaID)
	return "img-out", nil
}

func (f *fakeServices) SendCard(ctx context.Context, creds ttdomain.Credentials, conversationID string, card ttdomain.Card) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return "", f.sendErr
	}
	f.sentCards = append(f.sentCards, card)
	return "card-out", nil
}

func (f *fakeServices) SendAction(ctx context.Context, creds ttdomain.Credentials, conversationID string, action ttdomain.SenderAction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return nil
}

func (f *fakeServices) UploadImage(ctx context.Context, creds ttdomain.Credentials, data []byte, fileName, mimeType string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads++
	return "media-1", nil
}

func (f *fakeServices) DownloadMedia(ctx context.Context, creds ttdomain.Credentials, ref ttdomain.MediaRef) (ttdomain.DownloadedMedia, error) {
	return f.download, f.downloadErr
}

func (f *fakeServices) ImageAllowed(ctx context.Context, creds ttdomain.Credentials, conversationID string, t ttdomain.ConversationType) (bool, error) {
	return f.imageOK, f.imageErr
}

func (f *fakeServices) ProbeInbox(ctx context.Context, creds ttdomain.Credentials) error {
	return f.probeErr
}

func (f *fakeServices) Participants(ctx context.Context, creds ttdomain.Credentials, conversationID string) ([]ttdomain.Participant, error) {
	return f.people, f.peopleErr
}

func (f *fakeServices) ListVideos(ctx context.Context, creds ttdomain.Credentials, cursor int64, limit int) (ttdomain.VideoPage, error) {
	return f.videos, f.videosErr
}

func (f *fakeServices) GetVideos(ctx context.Context, creds ttdomain.Credentials, ids []string) ([]ttdomain.RemoteVideo, error) {
	return f.videos.Videos, f.videosErr
}

func (f *fakeServices) ListComments(ctx context.Context, creds ttdomain.Credentials, q ttdomain.CommentQuery) (ttdomain.CommentPage, error) {
	return f.commentPage, nil
}

func (f *fakeServices) LookupComments(ctx context.Context, creds ttdomain.Credentials, videoID string, ids []string) ([]ttdomain.RemoteComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookupCalls = append(f.lookupCalls, append([]string(nil), ids...))
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	var out []ttdomain.RemoteComment
	for _, id := range ids {
		if rc, ok := f.lookup[id]; ok {
			out = append(out, rc)
		}
	}
	return out, nil
}

func (f *fakeServices) Reply(ctx context.Context, creds ttdomain.Credentials, videoID, commentID, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replyErr != nil {
		return "", f.replyErr
	}
	f.replies = append(f.replies, sentReply{videoID, commentID, text})
	return fmt.Sprintf("9%d", len(f.replies)), nil
}

func (f *fakeServices) SetHidden(ctx context.Context, creds ttdomain.Credentials, videoID, commentID string, hidden bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden[commentID] = hidden
	return nil
}

func (f *fakeServices) SetLiked(ctx context.Context, creds ttdomain.Credentials, commentID string, liked bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.liked[commentID] = liked
	return nil
}

func (f *fakeServices) Subscription(ctx context.Context, kind ttdomain.WebhookKind) (*ttdomain.WebhookSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subErr != nil {
		return nil, f.subErr
	}
	return f.subscriptions[kind], nil
}

func (f *fakeServices) Subscribe(ctx context.Context, kind ttdomain.WebhookKind, callbackURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subErr != nil {
		return f.subErr
	}
	f.subscribed = append(f.subscribed, kind)
	f.subscriptions[kind] = &ttdomain.WebhookSubscription{Kind: kind, CallbackURL: callbackURL}
	return nil
}

type codedError struct {
	auth, permission, rate, budget, notFound, invalid, retry, ambiguous, refusal bool
}

func (e codedError) Error() string {
	return fmt.Sprintf("tiktok test error auth=%v permission=%v rate=%v budget=%v retry=%v ambiguous=%v refusal=%v", e.auth, e.permission, e.rate, e.budget, e.retry, e.ambiguous, e.refusal)
}

type fakeClassifier struct{}

func asCoded(err error) (codedError, bool) {
	c, ok := err.(codedError)
	return c, ok
}

func (fakeClassifier) IsAuth(err error) bool          { c, ok := asCoded(err); return ok && c.auth }
func (fakeClassifier) IsPermission(err error) bool    { c, ok := asCoded(err); return ok && c.permission }
func (fakeClassifier) IsRateLimited(err error) bool   { c, ok := asCoded(err); return ok && c.rate }
func (fakeClassifier) IsBudgetBlocked(err error) bool { c, ok := asCoded(err); return ok && c.budget }
func (fakeClassifier) IsNotFound(err error) bool      { c, ok := asCoded(err); return ok && c.notFound }
func (fakeClassifier) IsInvalidRequest(err error) bool {
	c, ok := asCoded(err)
	return ok && c.invalid
}
func (fakeClassifier) IsEligibility(err error) bool { c, ok := asCoded(err); return ok && c.permission }
func (fakeClassifier) IsRetryable(err error) bool   { c, ok := asCoded(err); return ok && c.retry }
func (fakeClassifier) IsAmbiguous(err error) bool   { c, ok := asCoded(err); return ok && c.ambiguous }
func (fakeClassifier) IsDefiniteRefusal(err error) bool {
	c, ok := asCoded(err)
	return ok && c.refusal
}

type fakeAlerts struct {
	mu        sync.Mutex
	reconnect []string
	soon      []string
	webhooks  []string
	healthy   []string
}

func (f *fakeAlerts) NeedsReconnect(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconnect = append(f.reconnect, a.ID)
	return nil
}

func (f *fakeAlerts) ReconnectSoon(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.soon = append(f.soon, a.ID)
	return nil
}

func (f *fakeAlerts) WebhooksUnhealthy(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.webhooks = append(f.webhooks, a.ID)
	return nil
}

func (f *fakeAlerts) Healthy(ctx context.Context, a *ttdomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthy = append(f.healthy, a.ID)
	return nil
}

type fakeMetrics struct {
	mu     sync.Mutex
	events map[string]int
}

func (f *fakeMetrics) AddTikTokEvent(stage, outcome string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		f.events = map[string]int{}
	}
	f.events[stage+":"+outcome]++
}

func (f *fakeMetrics) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events[key]
}

type fakeHistory struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
	err     error
}

func (f *fakeHistory) Record(ctx context.Context, r conversation.MessageHistoryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	for _, existing := range f.records {
		if r.ProviderMessageID != "" && existing.ProviderMessageID == r.ProviderMessageID && existing.EntryID == r.EntryID {
			return nil
		}
	}
	f.records = append(f.records, r)
	return nil
}

func (f *fakeHistory) all() []conversation.MessageHistoryRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]conversation.MessageHistoryRecord(nil), f.records...)
}

type fakeAssignments struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAssignments) EnsureAssignment(entryID, entryType, accountID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, entryID+":"+entryType+":"+accountID)
	return ""
}

type fakeDispatcher struct {
	mu     sync.Mutex
	inputs []conversation_usecase.InboundAutomationInput
	err    error
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	return f.err
}

type fakeObjects struct {
	mu       sync.Mutex
	uploaded map[string][]byte
	deleted  []string
	err      error
}

func (f *fakeObjects) UploadFile(key string, data []byte, contentType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.uploaded == nil {
		f.uploaded = map[string][]byte{}
	}
	f.uploaded[key] = data
	return nil
}

func (f *fakeObjects) GetFileURL(key string) string { return "https://media.example.com/" + key }

func (f *fakeObjects) DeleteFile(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, key)
	return nil
}

type fakeFetcher struct {
	data map[string][]byte
	err  error
}

func (f *fakeFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if d, ok := f.data[url]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", url)
}

func connectedAccount() *ttdomain.Account {
	expires := testNow.Add(20 * time.Hour)
	refresh := testNow.Add(300 * 24 * time.Hour)
	return &ttdomain.Account{
		ID: uuid.NewString(), WorkspaceID: "ws-1", OpenID: "_000abPL1cJIdVefgDhGijkl2mFn3AB0abY4", Status: ttdomain.StatusConnected,
		AccessToken: "act.1", RefreshToken: "rft.1", AccessExpiresAt: &expires, RefreshExpiresAt: &refresh, Username: "loja.oficial", DisplayName: "Loja Oficial",
		Capabilities:    ttdomain.AccountCapabilities{CanReadVideos: true, CanReadComments: true, CanManageComments: true, CanReceiveMessages: true, CanSendMessages: true, CanManageInboxProfile: true},
		WebhooksHealthy: true,
	}
}

type fakeSharedState struct {
	mu   sync.Mutex
	keys map[string]string
	err  error
}

func newFakeSharedState() *fakeSharedState { return &fakeSharedState{keys: map[string]string{}} }

func (f *fakeSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, ok := f.keys[key]; ok {
		return false, nil
	}
	f.keys[key] = value
	return true, nil
}

func (f *fakeSharedState) SetString(key, value string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.keys[key] = value
	return nil
}

func (f *fakeSharedState) GetString(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys[key], f.err
}

func (f *fakeSharedState) Del(keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.keys, k)
	}
	return nil
}

func (f *fakeSharedState) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	_, ok := f.keys[key]
	return ok, nil
}
