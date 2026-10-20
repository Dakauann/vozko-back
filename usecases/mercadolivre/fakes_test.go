package mercadolivre

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return testNow }

type mlErr struct {
	msg       string
	auth      bool
	perm      bool
	rate      bool
	blocked   bool
	substatus string
	notFound  bool
	ambiguous bool
	retryable bool
	operator  bool
	decode    bool
	appBlock  bool
	pending   bool
}

func (e *mlErr) Error() string { return "fake ml: " + e.msg }

type fakeClassifier struct{}

func as(err error) (*mlErr, bool) {
	var e *mlErr
	ok := errors.As(err, &e)
	return e, ok
}

func (fakeClassifier) IsAuth(err error) bool       { e, ok := as(err); return ok && e.auth }
func (fakeClassifier) IsPermission(err error) bool { e, ok := as(err); return ok && e.perm }
func (fakeClassifier) IsRateLimit(err error) bool  { e, ok := as(err); return ok && e.rate }
func (fakeClassifier) Blocked(err error) (string, bool) {
	e, ok := as(err)
	if !ok || !e.blocked {
		return "", false
	}
	return e.substatus, true
}
func (fakeClassifier) IsModerationPending(err error) bool { e, ok := as(err); return ok && e.pending }
func (fakeClassifier) IsAppBlocked(err error) bool        { e, ok := as(err); return ok && e.appBlock }
func (fakeClassifier) IsOperatorUser(err error) bool      { e, ok := as(err); return ok && e.operator }
func (fakeClassifier) IsNotFound(err error) bool          { e, ok := as(err); return ok && e.notFound }
func (fakeClassifier) IsAmbiguous(err error) bool         { e, ok := as(err); return ok && e.ambiguous }
func (fakeClassifier) IsRetryable(err error) bool {
	e, ok := as(err)
	return ok && (e.retryable || e.rate)
}
func (fakeClassifier) IsDecode(err error) bool { e, ok := as(err); return ok && e.decode }
func (fakeClassifier) IsDefiniteRefusal(err error) bool {
	e, ok := as(err)
	return ok && !e.ambiguous && !e.retryable
}

type fakeAccounts struct {
	mu        sync.Mutex
	rows      map[string]*mldomain.Account
	deleted   map[string]bool
	locked    map[string]bool
	seq       int
	failFind  error
	createErr error

	findByIDCalls  int
	findByIDsCalls int
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{rows: map[string]*mldomain.Account{}, deleted: map[string]bool{}, locked: map[string]bool{}}
}

func cloneAccount(a *mldomain.Account) *mldomain.Account {
	c := *a
	c.GrantedScopes = append([]string(nil), a.GrantedScopes...)
	return &c
}

func (f *fakeAccounts) put(a *mldomain.Account) *mldomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == "" {
		f.seq++
		a.ID = "acct-" + string(rune('0'+f.seq))
	}
	f.rows[a.ID] = cloneAccount(a)
	return a
}

func (f *fakeAccounts) get(id string) *mldomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.rows[id]; ok {
		return cloneAccount(a)
	}
	return nil
}

func (f *fakeAccounts) Create(_ context.Context, a *mldomain.Account) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mu.Lock()
	for id, row := range f.rows {
		if row.MLUserID == a.MLUserID && !f.deleted[id] {
			f.mu.Unlock()
			return mldomain.ErrAccountAlreadyLinked
		}
	}
	f.mu.Unlock()
	f.put(a)
	return nil
}

func (f *fakeAccounts) Update(_ context.Context, a *mldomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[a.ID]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	row.DepartmentID, row.Nickname = a.DepartmentID, a.Nickname
	row.AgentID, row.WorkflowID, row.PipelineID = a.AgentID, a.WorkflowID, a.PipelineID
	row.EnableAgentResponses, row.EnableWorkflow = a.EnableAgentResponses, a.EnableWorkflow
	row.AutomationDisclosure = a.AutomationDisclosure
	return nil
}

func (f *fakeAccounts) Reconnect(_ context.Context, a *mldomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[a.ID]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	delete(f.deleted, a.ID)
	row.SiteID, row.Nickname = a.SiteID, a.Nickname
	row.AccessToken, row.RefreshToken = a.AccessToken, a.RefreshToken
	row.AccessExpiresAt, row.RefreshExpiresAt = a.AccessExpiresAt, a.RefreshExpiresAt
	row.GrantedScopes = a.GrantedScopes
	row.SetCapabilities(a.Capabilities())
	row.Status, row.StatusReason, row.PurgeAfter = a.Status, a.StatusReason, nil
	return nil
}

func (f *fakeAccounts) UpdateStatus(_ context.Context, id string, status mldomain.Status, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	row.Status, row.StatusReason = status, reason
	if status != mldomain.StatusConnected {
		row.AccessToken, row.RefreshToken, row.AccessExpiresAt = "", "", nil
	}
	return nil
}

func (f *fakeAccounts) UpdateCapabilities(_ context.Context, id string, caps mldomain.AccountCapabilities) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	row.SetCapabilities(caps)
	return nil
}

func (f *fakeAccounts) UpdateNickname(_ context.Context, id, nickname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	row.Nickname = nickname
	return nil
}

func (f *fakeAccounts) FindByID(_ context.Context, id string) (*mldomain.Account, error) {
	if f.failFind != nil {
		return nil, f.failFind
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findByIDCalls++
	row, ok := f.rows[id]
	if !ok || f.deleted[id] {
		return nil, mldomain.ErrAccountNotFound
	}
	return cloneAccount(row), nil
}

func (f *fakeAccounts) FindByIDs(_ context.Context, ids []string) ([]*mldomain.Account, error) {
	if f.failFind != nil {
		return nil, f.failFind
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findByIDsCalls++
	out := make([]*mldomain.Account, 0, len(ids))
	for _, id := range ids {
		if row, ok := f.rows[id]; ok && !f.deleted[id] {
			out = append(out, cloneAccount(row))
		}
	}
	return out, nil
}

func (f *fakeAccounts) FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*mldomain.Account, error) {
	a, err := f.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.WorkspaceID != workspaceID {
		return nil, mldomain.ErrAccountNotFound
	}
	return a, nil
}

func (f *fakeAccounts) FindLiveByMLUserID(_ context.Context, mlUserID string) (*mldomain.Account, error) {
	if f.failFind != nil {
		return nil, f.failFind
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, row := range f.rows {
		if row.MLUserID == mlUserID && !f.deleted[id] {
			return cloneAccount(row), nil
		}
	}
	return nil, mldomain.ErrAccountNotFound
}

func (f *fakeAccounts) FindByWorkspaceAndMLUserIDUnscoped(_ context.Context, workspaceID, mlUserID string) (*mldomain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.MLUserID == mlUserID && row.WorkspaceID == workspaceID {
			return cloneAccount(row), nil
		}
	}
	return nil, mldomain.ErrAccountNotFound
}

func (f *fakeAccounts) Restore(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.deleted, id)
	return nil
}

func (f *fakeAccounts) ListByWorkspace(_ context.Context, in mldomain.ListAccountsInput) (*shared.PaginatedResult[*mldomain.Account], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []*mldomain.Account
	for id, row := range f.rows {
		if row.WorkspaceID == in.WorkspaceID && !f.deleted[id] {
			items = append(items, cloneAccount(row))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return shared.NewPaginatedResult(items, shared.NormalizePagination(in.Options.Pagination), int64(len(items))), nil
}

func (f *fakeAccounts) due(filter func(a *mldomain.Account) bool, limit int) []*mldomain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*mldomain.Account
	for id, row := range f.rows {
		if !f.deleted[id] && filter(row) {
			out = append(out, cloneAccount(row))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakeAccounts) ListDueForRefresh(_ context.Context, before time.Time, limit int) ([]*mldomain.Account, error) {
	return f.due(func(a *mldomain.Account) bool {
		return a.Status == mldomain.StatusConnected && a.RefreshToken != "" && (a.AccessExpiresAt == nil || a.AccessExpiresAt.Before(before))
	}, limit), nil
}

func (f *fakeAccounts) LockForRefresh(ctx context.Context, id string, refresh mldomain.RefreshFunc) (bool, error) {
	f.mu.Lock()
	row, ok := f.rows[id]
	if !ok || f.deleted[id] || f.locked[id] {
		f.mu.Unlock()
		return false, nil
	}
	f.locked[id] = true
	snapshot := cloneAccount(row)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.locked, id)
		f.mu.Unlock()
	}()
	outcome, err := refresh(ctx, snapshot)
	if err != nil {
		return true, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if outcome.Status != "" {
		row.Status, row.StatusReason = outcome.Status, outcome.StatusReason
	}
	if t := outcome.Tokens; t != nil {
		row.AccessToken, row.RefreshToken = t.AccessToken, t.RefreshToken
		at, rt := t.AccessExpiresAt, t.RefreshExpiresAt
		row.AccessExpiresAt, row.RefreshExpiresAt = &at, &rt
	} else if outcome.Status != "" && outcome.Status != mldomain.StatusConnected {
		row.AccessToken, row.RefreshToken, row.AccessExpiresAt = "", "", nil
	}
	return true, nil
}

func (f *fakeAccounts) ListDueForReconcile(_ context.Context, before time.Time, limit int) ([]*mldomain.Account, error) {
	return f.due(func(a *mldomain.Account) bool {
		return a.Status == mldomain.StatusConnected && (a.LastReconciledAt == nil || a.LastReconciledAt.Before(before))
	}, limit), nil
}

func (f *fakeAccounts) MarkReconciled(_ context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[id]; ok {
		row.LastReconciledAt = &at
	}
	return nil
}

func (f *fakeAccounts) ListDueForHealthCheck(_ context.Context, before time.Time, limit int) ([]*mldomain.Account, error) {
	return f.due(func(a *mldomain.Account) bool {
		live := a.Status == mldomain.StatusConnected || a.Status == mldomain.StatusNeedsReconnect || a.Status == mldomain.StatusRevoked
		return live && (a.LastHealthCheckAt == nil || a.LastHealthCheckAt.Before(before))
	}, limit), nil
}

func (f *fakeAccounts) MarkHealthChecked(_ context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[id]; ok {
		row.LastHealthCheckAt = &at
	}
	return nil
}

func (f *fakeAccounts) Disconnect(_ context.Context, id string, status mldomain.Status, reason string, purgeAfter time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return mldomain.ErrAccountNotFound
	}
	row.Status, row.StatusReason = status, reason
	row.AccessToken, row.RefreshToken, row.AccessExpiresAt = "", "", nil
	row.PurgeAfter = &purgeAfter
	f.deleted[id] = true
	return nil
}

func (f *fakeAccounts) ListDueForPurge(context.Context, time.Time, int) ([]*mldomain.Account, error) {
	return nil, nil
}

func (f *fakeAccounts) DeletePurged(context.Context, string) error { return nil }

type fakeContacts struct {
	mu     sync.Mutex
	rows   map[string]*mldomain.Contact
	seq    int
	marks  int
	clears int
}

func newFakeContacts() *fakeContacts { return &fakeContacts{rows: map[string]*mldomain.Contact{}} }

func (f *fakeContacts) FindOrCreateByUserID(_ context.Context, in mldomain.FindOrCreateContactInput) (*mldomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.AccountID == in.AccountID && c.MLUserID == in.MLUserID {
			cp := *c
			return &cp, nil
		}
	}
	f.seq++
	c := &mldomain.Contact{ID: "ct-" + string(rune('0'+f.seq)), WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, MLUserID: in.MLUserID, Nickname: in.Nickname}
	f.rows[c.ID] = c
	cp := *c
	return &cp, nil
}

func (f *fakeContacts) FindByID(_ context.Context, id string) (*mldomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.rows[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, mldomain.ErrContactNotFound
}

func (f *fakeContacts) FindByIDs(ctx context.Context, ids []string) ([]*mldomain.Contact, error) {
	var out []*mldomain.Contact
	for _, id := range ids {
		if c, err := f.FindByID(ctx, id); err == nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeContacts) FindByUserID(_ context.Context, accountID, mlUserID string) (*mldomain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.AccountID == accountID && c.MLUserID == mlUserID {
			cp := *c
			return &cp, nil
		}
	}
	return nil, mldomain.ErrContactNotFound
}

func (f *fakeContacts) UpdateNickname(_ context.Context, id, nickname string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.rows[id]; ok {
		c.Nickname, c.ProfileRefreshedAt = nickname, &at
	}
	return nil
}

func (f *fakeContacts) SetLeadID(context.Context, string, *string) error { return nil }

func (f *fakeContacts) SetQuestionsBlocked(_ context.Context, id string, at *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[id]
	if !ok {
		return mldomain.ErrContactNotFound
	}
	c.QuestionsBlockedAt = at
	return nil
}

func (f *fakeContacts) MarkQuestionsBlocked(_ context.Context, accountID string, mlUserIDs []string, at, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marks++
	listed := map[string]bool{}
	for _, id := range mlUserIDs {
		listed[id] = true
	}
	for _, c := range f.rows {
		if c.AccountID == accountID && listed[c.MLUserID] && c.QuestionsBlockedAt == nil {
			blockedAt := at
			c.QuestionsBlockedAt = &blockedAt
		}
	}
	return nil
}

func (f *fakeContacts) ClearQuestionsBlockedExcept(_ context.Context, accountID string, keep []string, blockedBefore time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
	kept := map[string]bool{}
	for _, id := range keep {
		kept[id] = true
	}
	for _, c := range f.rows {
		if c.AccountID == accountID && !kept[c.MLUserID] && c.QuestionsBlockedAt != nil && c.QuestionsBlockedAt.Before(blockedBefore) {
			c.QuestionsBlockedAt = nil
		}
	}
	return nil
}

type fakeConversations struct {
	mu        sync.Mutex
	rows      map[string]*mldomain.Conversation
	seq       int
	claims    int
	releases  int
	inbound   []time.Time
	outbound  int
	states    []mldomain.ThreadState
	summaries []mldomain.PackSummary
	claimErr  error

	findByIDCalls  int
	findByIDsCalls int
	findByIDsErr   error
}

func newFakeConversations() *fakeConversations {
	return &fakeConversations{rows: map[string]*mldomain.Conversation{}}
}

func cloneConv(c *mldomain.Conversation) *mldomain.Conversation {
	cp := *c
	return &cp
}

func (f *fakeConversations) put(c *mldomain.Conversation) *mldomain.Conversation {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[c.ID] = cloneConv(c)
	return c
}

func (f *fakeConversations) get(id string) *mldomain.Conversation {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.rows[id]; ok {
		return cloneConv(c)
	}
	return nil
}

func (f *fakeConversations) FindOrCreateByPack(_ context.Context, in mldomain.FindOrCreateConversationInput) (*mldomain.Conversation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.AccountID == in.AccountID && c.PackKey == in.PackKey {
			return cloneConv(c), false, nil
		}
	}
	f.seq++
	c := &mldomain.Conversation{ID: "conv-" + string(rune('0'+f.seq)), WorkspaceID: in.WorkspaceID, AccountID: in.AccountID, ContactID: in.ContactID,
		PackKey: in.PackKey, OrderIDs: in.OrderIDs, BuyerMLUserID: in.BuyerMLUserID}
	f.rows[c.ID] = c
	return cloneConv(c), true, nil
}

func (f *fakeConversations) FindByID(_ context.Context, id string) (*mldomain.Conversation, error) {
	f.mu.Lock()
	f.findByIDCalls++
	f.mu.Unlock()
	if c := f.get(id); c != nil {
		return c, nil
	}
	return nil, mldomain.ErrConversationNotFound
}

func (f *fakeConversations) FindByIDs(ctx context.Context, ids []string) ([]*mldomain.Conversation, error) {
	f.mu.Lock()
	f.findByIDsCalls++
	f.mu.Unlock()
	if f.findByIDsErr != nil {
		return nil, f.findByIDsErr
	}
	var out []*mldomain.Conversation
	for _, id := range ids {
		if c := f.get(id); c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeConversations) FindByPack(_ context.Context, accountID, packKey string) (*mldomain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.AccountID == accountID && c.PackKey == packKey {
			return cloneConv(c), nil
		}
	}
	return nil, mldomain.ErrConversationNotFound
}

func (f *fakeConversations) FindByShipment(_ context.Context, accountID, shipmentID string) (*mldomain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.rows {
		if c.AccountID == accountID && c.PackSummary != nil && c.PackSummary.ShipmentID == shipmentID {
			return cloneConv(c), nil
		}
	}
	return nil, mldomain.ErrConversationNotFound
}

func (f *fakeConversations) WorkspaceIDForEntry(_ context.Context, id string) (string, error) {
	if c := f.get(id); c != nil {
		return c.WorkspaceID, nil
	}
	return "", mldomain.ErrConversationNotFound
}

func (f *fakeConversations) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversations) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeConversations) ApplyStatus(_ context.Context, id string, state mldomain.ThreadState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, state)
	if c, ok := f.rows[id]; ok {
		c.MLStatus, c.MLSubstatus, c.AgentMediated = state.Status, state.Substatus, state.AgentMediated
		c.OutboundBudget, c.WindowOpen = state.OutboundBudget, state.WindowOpen
		at := state.ReadAt
		c.MLStatusReadAt = &at
	}
	return nil
}

func (f *fakeConversations) ClaimOutbound(_ context.Context, id string) (bool, error) {
	if f.claimErr != nil {
		return false, f.claimErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[id]
	if !ok || !c.WindowOpen || (c.OutboundBudget > 0 && c.OutboundSinceInbound >= c.OutboundBudget) {
		return false, nil
	}
	c.OutboundSinceInbound++
	f.claims++
	return true, nil
}

func (f *fakeConversations) ReleaseOutbound(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	if c, ok := f.rows[id]; ok && c.OutboundSinceInbound > 0 {
		c.OutboundSinceInbound--
	}
	return nil
}

func (f *fakeConversations) RecordInbound(_ context.Context, id string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inbound = append(f.inbound, at)
	if c, ok := f.rows[id]; ok {
		c.OutboundSinceInbound = 0
		c.LastCustomerMessageAt = &at
	}
	return nil
}

func (f *fakeConversations) RecordOutbound(context.Context, string, time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outbound++
	return nil
}

func (f *fakeConversations) SetStatus(context.Context, string, conversation.StatusWrite) error {
	return nil
}

func (f *fakeConversations) SetAutomationEnabled(context.Context, string, *bool) error { return nil }

func (f *fakeConversations) StatusForEntry(context.Context, string) (string, error) { return "", nil }

func (f *fakeConversations) UpdatePackSummary(_ context.Context, id string, summary mldomain.PackSummary, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summaries = append(f.summaries, summary)
	if c, ok := f.rows[id]; ok {
		s := summary
		c.PackSummary, c.PackSummaryRefreshedAt = &s, &at
	}
	return nil
}

func (f *fakeConversations) SetBuyer(_ context.Context, id, buyer string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.rows[id]; ok {
		c.BuyerMLUserID = buyer
	}
	return nil
}

func (f *fakeConversations) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}

type fakeOAuth struct {
	mu          sync.Mutex
	exchange    *mldomain.TokenGrant
	exchangeErr error
	refresh     func(creds mldomain.RefreshCredentials) (*mldomain.TokenGrant, error)
	refreshes   int
	revoked     []mldomain.Credentials
	revokeErr   error
	verifier    string
}

func (f *fakeOAuth) AuthorizeURL(state, challenge string) (string, error) {
	return "https://auth.mercadolivre.com.br/authorization?state=" + state + "&code_challenge=" + challenge, nil
}

func (f *fakeOAuth) ExchangeCode(_ context.Context, code, verifier string) (*mldomain.TokenGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifier = verifier
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	g := *f.exchange
	return &g, nil
}

func (f *fakeOAuth) Refresh(_ context.Context, creds mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
	f.mu.Lock()
	f.refreshes++
	fn := f.refresh
	f.mu.Unlock()
	return fn(creds)
}

func (f *fakeOAuth) Revoke(_ context.Context, creds mldomain.Credentials) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, creds)
	return f.revokeErr
}

type fakeUsers struct {
	me      *mldomain.User
	meErr   error
	users   map[string]*mldomain.User
	lookups int
}

func (f *fakeUsers) Me(context.Context, mldomain.Credentials) (*mldomain.User, error) {
	if f.meErr != nil {
		return nil, f.meErr
	}
	u := *f.me
	return &u, nil
}

func (f *fakeUsers) GetUser(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.User, error) {
	f.lookups++
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return nil, &mlErr{msg: "user", notFound: true}
}

type fakeQuestions struct {
	searchErr     error
	searches      int
	blacklist     []mldomain.BlacklistPage
	blacklistErr  error
	blacklistAsks []int
	priorities    []bool
}

func (f *fakeQuestions) QuestionBlacklist(ctx context.Context, _ mldomain.Credentials, offset, limit int) (*mldomain.BlacklistPage, error) {
	f.blacklistAsks = append(f.blacklistAsks, offset)
	f.priorities = append(f.priorities, mldomain.IsBackground(ctx))
	if f.blacklistErr != nil {
		return nil, f.blacklistErr
	}
	idx := len(f.blacklistAsks) - 1
	if idx >= len(f.blacklist) {
		return &mldomain.BlacklistPage{Offset: offset, Limit: limit}, nil
	}
	page := f.blacklist[idx]
	return &page, nil
}

func (f *fakeQuestions) GetQuestion(context.Context, mldomain.Credentials, string) (*mldomain.RemoteQuestion, error) {
	return nil, errors.New("not used")
}

func (f *fakeQuestions) SearchQuestions(context.Context, mldomain.Credentials, mldomain.QuestionSearch) (*mldomain.QuestionPage, error) {
	f.searches++
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return &mldomain.QuestionPage{}, nil
}

func (f *fakeQuestions) Answer(context.Context, mldomain.Credentials, string, string) (*mldomain.RemoteQuestion, error) {
	return nil, errors.New("not used")
}
func (f *fakeQuestions) DeleteQuestion(context.Context, mldomain.Credentials, string) error {
	return errors.New("not used")
}
func (f *fakeQuestions) BlockAsker(context.Context, mldomain.Credentials, string) error {
	return errors.New("not used")
}
func (f *fakeQuestions) UnblockAsker(context.Context, mldomain.Credentials, string) error {
	return errors.New("not used")
}
func (f *fakeQuestions) ResponseTime(context.Context, mldomain.Credentials) (*mldomain.ResponseTime, error) {
	return nil, errors.New("not used")
}

type fakeMessages struct {
	mu        sync.Mutex
	threads   map[string]*mldomain.Thread
	threadErr error
	messages  map[string]*mldomain.RemoteMessage
	getErr    error
	sent      []mldomain.SendMessage
	sendOut   *mldomain.SentMessage
	sendErr   error
	unread    []mldomain.UnreadThread
	unreadErr error
	reads     []mldomain.ThreadQuery
	ctxs      []context.Context
}

func (f *fakeMessages) Thread(ctx context.Context, _ mldomain.Credentials, q mldomain.ThreadQuery) (*mldomain.Thread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, q)
	f.ctxs = append(f.ctxs, ctx)
	if f.threadErr != nil {
		return nil, f.threadErr
	}
	th, ok := f.threads[q.PackKey]
	if !ok {
		return nil, &mlErr{msg: "thread", notFound: true}
	}
	cp := *th
	start := min(q.Offset, len(th.Messages))
	end := min(start+max(q.Limit, 1), len(th.Messages))
	cp.Messages = th.Messages[start:end]
	cp.Total = len(th.Messages)
	return &cp, nil
}

func (f *fakeMessages) ThreadForOperator(context.Context, mldomain.Credentials, mldomain.ThreadQuery) (*mldomain.Thread, error) {
	return nil, errors.New("background reads never mark as read")
}

func (f *fakeMessages) GetMessage(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.RemoteMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	m, ok := f.messages[id]
	if !ok {
		return nil, &mlErr{msg: "message", notFound: true}
	}
	cp := *m
	return &cp, nil
}

func (f *fakeMessages) Send(_ context.Context, _ mldomain.Credentials, msg mldomain.SendMessage) (*mldomain.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	out := *f.sendOut
	return &out, nil
}

func (f *fakeMessages) Unread(context.Context, mldomain.Credentials) ([]mldomain.UnreadThread, error) {
	if f.unreadErr != nil {
		return nil, f.unreadErr
	}
	return f.unread, nil
}

type fakeAttachments struct {
	uploads   []mldomain.AttachmentUpload
	uploadErr error
	files     map[string]*mldomain.AttachmentDownload
}

func (f *fakeAttachments) Upload(_ context.Context, _ mldomain.Credentials, up mldomain.AttachmentUpload) (string, error) {
	f.uploads = append(f.uploads, up)
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	return "att-" + up.FileName, nil
}

func (f *fakeAttachments) Download(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.AttachmentDownload, error) {
	if d, ok := f.files[id]; ok {
		return d, nil
	}
	return nil, &mlErr{msg: "attachment", retryable: true}
}

type fakeSales struct {
	packs     map[string]*mldomain.RemotePack
	orders    map[string]*mldomain.RemoteOrder
	shipments map[string]*mldomain.RemoteShipment
	calls     int
}

func (f *fakeSales) GetPack(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.RemotePack, error) {
	f.calls++
	if p, ok := f.packs[id]; ok {
		return p, nil
	}
	return nil, &mlErr{msg: "pack", notFound: true}
}

func (f *fakeSales) GetOrder(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.RemoteOrder, error) {
	f.calls++
	if o, ok := f.orders[id]; ok {
		return o, nil
	}
	return nil, &mlErr{msg: "order", notFound: true}
}

func (f *fakeSales) GetShipment(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.RemoteShipment, error) {
	f.calls++
	if s, ok := f.shipments[id]; ok {
		return s, nil
	}
	return nil, &mlErr{msg: "shipment", notFound: true}
}

type fakeMissedFeeds struct {
	pages map[mldomain.Topic][][]mldomain.Notification
	calls []mldomain.MissedFeedQuery
	err   error
}

func (f *fakeMissedFeeds) MissedFeeds(_ context.Context, _ mldomain.Credentials, q mldomain.MissedFeedQuery) ([]mldomain.Notification, error) {
	f.calls = append(f.calls, q)
	if f.err != nil {
		return nil, f.err
	}
	pages := f.pages[q.Topic]
	idx := q.Offset / max(q.Limit, 1)
	if idx >= len(pages) {
		return nil, nil
	}
	return pages[idx], nil
}

type fakeAlerts struct {
	mu          sync.Mutex
	alerted     []string
	reconnected []string
}

func (f *fakeAlerts) NeedsReconnect(_ context.Context, a *mldomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerted = append(f.alerted, a.ID)
	return nil
}

func (f *fakeAlerts) Reconnected(_ context.Context, a *mldomain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconnected = append(f.reconnected, a.ID)
	return nil
}

type fakeMetrics struct {
	mu     sync.Mutex
	events map[string]int
}

func newFakeMetrics() *fakeMetrics { return &fakeMetrics{events: map[string]int{}} }

func (f *fakeMetrics) AddMercadoLivreEvent(stage, outcome string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events[stage+"/"+outcome]++
}

func (f *fakeMetrics) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events[key]
}

type fakePublisher struct {
	mu        sync.Mutex
	published map[string][][]byte
	err       error
}

func newFakePublisher() *fakePublisher { return &fakePublisher{published: map[string][][]byte{}} }

func (f *fakePublisher) Publish(topic string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.published[topic] = append(f.published[topic], payload)
	return nil
}

func (f *fakePublisher) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.published {
		n += len(p)
	}
	return n
}

type fakeHistory struct {
	mu      sync.Mutex
	records []conversation.MessageHistoryRecord
	err     error
}

func (f *fakeHistory) Record(_ context.Context, r conversation.MessageHistoryRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, r)
	return nil
}

type fakeMessageStore struct {
	mu      sync.Mutex
	byExt   map[string]*conversation.Message
	updated []*conversation.Message
}

func newFakeMessageStore() *fakeMessageStore {
	return &fakeMessageStore{byExt: map[string]*conversation.Message{}}
}

func (f *fakeMessageStore) GetByEntryAndExternalMessageID(_ shared.EntryType, entryID, ext string) (*conversation.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.byExt[entryID+"|"+ext]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func (f *fakeMessageStore) Update(_ string, m *conversation.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *m
	f.updated = append(f.updated, &cp)
	if m.ExternalMessageID != nil {
		f.byExt[m.EntryID+"|"+*m.ExternalMessageID] = &cp
	}
	return nil
}

type fakeMedia struct {
	stored []conversation.StoreMediaInput
	err    error
}

func (f *fakeMedia) Store(in conversation.StoreMediaInput) (*conversation.ConversationMedia, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.stored = append(f.stored, in)
	return &conversation.ConversationMedia{ID: in.ID, Type: in.Type, URL: "https://cdn.example/" + in.Key}, nil
}

type fakeAssignments struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAssignments) EnsureAssignment(entryID, entryType, accountID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, entryID+"|"+entryType+"|"+accountID)
	return ""
}

type fakeDispatcher struct {
	mu     sync.Mutex
	inputs []conversation_usecase.InboundAutomationInput
}

func (f *fakeDispatcher) Dispatch(_ context.Context, in conversation_usecase.InboundAutomationInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	return nil
}

type fakeWatermarks struct {
	calls []string
}

func (f *fakeWatermarks) MarkOutboundStatusUpTo(entryID string, entryType shared.EntryType, status conversation.DeliveryStatus, upTo time.Time) (int64, error) {
	f.calls = append(f.calls, entryID+"|"+string(entryType)+"|"+string(status)+"|"+upTo.Format(time.RFC3339))
	return 1, nil
}

type fakeBroadcaster struct {
	mu      sync.Mutex
	updates []string
}

func (f *fakeBroadcaster) BroadcastMessageUpdated(entryID, entryType string, _ *conversation.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, "message|"+entryID)
}

func (f *fakeBroadcaster) BroadcastEntryUpdate(entryID, entryType string, _ *conversation.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, "entry|"+entryID)
}

func connectedAccount() *mldomain.Account {
	expires := testNow.Add(5 * time.Hour)
	refresh := testNow.Add(150 * 24 * time.Hour)
	return &mldomain.Account{
		ID: "acct-1", WorkspaceID: "ws-1", MLUserID: "415458330", SiteID: mldomain.SiteMLB, Nickname: "LOJA",
		AccessToken: "APP_USR-1", RefreshToken: "TG-1", AccessExpiresAt: &expires, RefreshExpiresAt: &refresh,
		GrantedScopes: mldomain.RequiredScopes(), Status: mldomain.StatusConnected,
		CanReadMessages: true, CanSendMessages: true, CanReadQuestions: true, CanAnswerQuestions: true,
	}
}

func boolPtr(b bool) *bool { return &b }

func strPtr(s string) *string { return &s }

func hasPrefix(values []string, prefix string) bool {
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

type fakeInboundClaims struct {
	mu      sync.Mutex
	claimed map[string]bool
	err     error
}

func (f *fakeInboundClaims) Claim(_ context.Context, key, channel, accountID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	if f.claimed[key] {
		return false, nil
	}
	f.claimed[key] = true
	return true, nil
}
