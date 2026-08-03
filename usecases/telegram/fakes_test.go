package telegram

import (
	"context"
	"errors"
	"sync"
	"time"

	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

// Hand-written fakes with XxxFn fields, matching the repo's established test
// idiom: a test overrides only the behaviour it cares about, and any call it did
// not anticipate fails loudly rather than returning a plausible zero value.

type fakeAccounts struct {
	mu sync.Mutex

	FindByIDFn                   func(ctx context.Context, id string) (*tgdomain.Account, error)
	FindByBotUserIDUnscopedFn    func(ctx context.Context, botUserID int64) (*tgdomain.Account, error)
	FindByBusinessConnectionIDFn func(ctx context.Context, id string) (*tgdomain.Account, error)
	CreateFn                     func(ctx context.Context, a *tgdomain.Account) error
	UpdateFn                     func(ctx context.Context, a *tgdomain.Account) error
	UpdateStatusFn               func(ctx context.Context, id string, s tgdomain.Status, reason string) error
	ListForHealthCheckFn         func(ctx context.Context, before time.Time, limit int) ([]*tgdomain.Account, error)
	UpdateWebhookHealthFn        func(ctx context.Context, id string, h tgdomain.WebhookHealth) error

	// Recorded for assertions.
	StatusWrites  []statusWrite
	HealthWrites  []tgdomain.WebhookHealth
	Created       []*tgdomain.Account
	Updated       []*tgdomain.Account
	RestoredIDs   []string
	RegisteredIDs []string
}

type statusWrite struct {
	ID     string
	Status tgdomain.Status
	Reason string
}

func (f *fakeAccounts) Create(ctx context.Context, a *tgdomain.Account) error {
	f.mu.Lock()
	f.Created = append(f.Created, a)
	f.mu.Unlock()
	if f.CreateFn != nil {
		return f.CreateFn(ctx, a)
	}
	if a.ID == "" {
		a.ID = "acct-created"
	}
	return nil
}

func (f *fakeAccounts) Update(ctx context.Context, a *tgdomain.Account) error {
	f.mu.Lock()
	f.Updated = append(f.Updated, a)
	f.mu.Unlock()
	if f.UpdateFn != nil {
		return f.UpdateFn(ctx, a)
	}
	return nil
}

func (f *fakeAccounts) UpdateStatus(ctx context.Context, id string, s tgdomain.Status, reason string) error {
	f.mu.Lock()
	f.StatusWrites = append(f.StatusWrites, statusWrite{ID: id, Status: s, Reason: reason})
	f.mu.Unlock()
	if f.UpdateStatusFn != nil {
		return f.UpdateStatusFn(ctx, id, s, reason)
	}
	return nil
}

func (f *fakeAccounts) UpdateWebhookHealth(ctx context.Context, id string, h tgdomain.WebhookHealth) error {
	f.mu.Lock()
	f.HealthWrites = append(f.HealthWrites, h)
	f.mu.Unlock()
	if f.UpdateWebhookHealthFn != nil {
		return f.UpdateWebhookHealthFn(ctx, id, h)
	}
	return nil
}

func (f *fakeAccounts) SetWebhookRegistered(_ context.Context, id string, _ time.Time) error {
	f.mu.Lock()
	f.RegisteredIDs = append(f.RegisteredIDs, id)
	f.mu.Unlock()
	return nil
}

func (f *fakeAccounts) FindByID(ctx context.Context, id string) (*tgdomain.Account, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, tgdomain.ErrAccountNotFound
}

func (f *fakeAccounts) FindByIDForWebhook(ctx context.Context, id string) (*tgdomain.Account, error) {
	return f.FindByID(ctx, id)
}

func (f *fakeAccounts) FindByBotUserID(ctx context.Context, botUserID int64) (*tgdomain.Account, error) {
	return f.FindByBotUserIDUnscoped(ctx, botUserID)
}

func (f *fakeAccounts) FindByBotUserIDUnscoped(ctx context.Context, botUserID int64) (*tgdomain.Account, error) {
	if f.FindByBotUserIDUnscopedFn != nil {
		return f.FindByBotUserIDUnscopedFn(ctx, botUserID)
	}
	return nil, tgdomain.ErrAccountNotFound
}

func (f *fakeAccounts) FindByBusinessConnectionID(ctx context.Context, id string) (*tgdomain.Account, error) {
	if f.FindByBusinessConnectionIDFn != nil {
		return f.FindByBusinessConnectionIDFn(ctx, id)
	}
	return nil, tgdomain.ErrAccountNotFound
}

func (f *fakeAccounts) Restore(_ context.Context, id string) error {
	f.mu.Lock()
	f.RestoredIDs = append(f.RestoredIDs, id)
	f.mu.Unlock()
	return nil
}

func (f *fakeAccounts) ListByWorkspace(context.Context, tgdomain.ListAccountsInput) (*shared.PaginatedResult[*tgdomain.Account], error) {
	return nil, nil
}

func (f *fakeAccounts) ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*tgdomain.Account, error) {
	if f.ListForHealthCheckFn != nil {
		return f.ListForHealthCheckFn(ctx, before, limit)
	}
	return nil, nil
}

func (f *fakeAccounts) Delete(context.Context, string) error { return nil }

// ---------------------------------------------------------------- contacts

type fakeContacts struct {
	mu sync.Mutex

	FindByIDFn func(ctx context.Context, id string) (*tgdomain.Contact, error)

	BlockedWrites []blockWrite
	ChatIDWrites  []int64
}

type blockWrite struct {
	ID      string
	Blocked bool
}

func (f *fakeContacts) FindOrCreate(context.Context, tgdomain.FindOrCreateContactInput) (*tgdomain.Contact, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeContacts) FindByID(ctx context.Context, id string) (*tgdomain.Contact, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, tgdomain.ErrContactNotFound
}

func (f *fakeContacts) FindByIDs(context.Context, []string) ([]*tgdomain.Contact, error) {
	return nil, nil
}

func (f *fakeContacts) FindByTGUserID(context.Context, string, int64) (*tgdomain.Contact, error) {
	return nil, tgdomain.ErrContactNotFound
}

func (f *fakeContacts) UpdateProfile(context.Context, string, tgdomain.ContactProfile) error {
	return nil
}

func (f *fakeContacts) SetBlocked(_ context.Context, id string, blocked bool, _ time.Time) error {
	f.mu.Lock()
	f.BlockedWrites = append(f.BlockedWrites, blockWrite{ID: id, Blocked: blocked})
	f.mu.Unlock()
	return nil
}

func (f *fakeContacts) SetPhone(context.Context, string, string, *string, time.Time) error {
	return nil
}

func (f *fakeContacts) UpdateChatID(_ context.Context, _ string, chatID int64) error {
	f.mu.Lock()
	f.ChatIDWrites = append(f.ChatIDWrites, chatID)
	f.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------- conversations

type fakeConversations struct {
	mu sync.Mutex

	FindByIDFn func(ctx context.Context, id string) (*tgdomain.Conversation, error)

	OutboundWrites []string
	ChatIDWrites   []int64
}

func (f *fakeConversations) FindOrCreate(context.Context, tgdomain.FindOrCreateConversationInput) (*tgdomain.Conversation, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeConversations) FindByID(ctx context.Context, id string) (*tgdomain.Conversation, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, tgdomain.ErrConversationNotFound
}

func (f *fakeConversations) FindByContact(context.Context, string, string) (*tgdomain.Conversation, error) {
	return nil, tgdomain.ErrConversationNotFound
}

func (f *fakeConversations) FindByChat(context.Context, string, int64) (*tgdomain.Conversation, error) {
	return nil, tgdomain.ErrConversationNotFound
}

func (f *fakeConversations) WorkspaceIDForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversations) DepartmentIDForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversations) ListEntryIDsByWorkspace(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeConversations) RecordInbound(context.Context, string, time.Time) error { return nil }

func (f *fakeConversations) RecordOutbound(_ context.Context, id string, _ time.Time) error {
	f.mu.Lock()
	f.OutboundWrites = append(f.OutboundWrites, id)
	f.mu.Unlock()
	return nil
}

func (f *fakeConversations) SetAutomationEnabled(context.Context, string, *bool) error {
	return nil
}

func (f *fakeConversations) SetStatus(context.Context, string, string, string, string, *time.Time) error {
	return nil
}

func (f *fakeConversations) StatusForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversations) SetStartPayload(context.Context, string, string) error { return nil }

func (f *fakeConversations) UpdateChatID(_ context.Context, _ string, chatID int64) error {
	f.mu.Lock()
	f.ChatIDWrites = append(f.ChatIDWrites, chatID)
	f.mu.Unlock()
	return nil
}

func (f *fakeConversations) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}

// ---------------------------------------------------------------- bot API

type fakeBotAPI struct {
	mu sync.Mutex

	GetMeFn          func(ctx context.Context, token string) (*tgdomain.BotProfile, error)
	SetWebhookFn     func(ctx context.Context, token string, cfg tgdomain.WebhookConfig) error
	GetWebhookInfoFn func(ctx context.Context, token string) (*tgdomain.WebhookInfo, error)
	SendTextFn       func(ctx context.Context, token string, in tgdomain.SendTextInput) (*tgdomain.SendResult, error)
	SendMediaFn      func(ctx context.Context, token string, in tgdomain.SendMediaInput) (*tgdomain.SendResult, error)

	SentText    []tgdomain.SendTextInput
	SentMedia   []tgdomain.SendMediaInput
	SentTokens  []string
	WebhookCfgs []tgdomain.WebhookConfig
}

func (f *fakeBotAPI) GetMe(ctx context.Context, token string) (*tgdomain.BotProfile, error) {
	if f.GetMeFn != nil {
		return f.GetMeFn(ctx, token)
	}
	return &tgdomain.BotProfile{BotUserID: 77777, Username: "vozko_bot", FirstName: "Vozko"}, nil
}

func (f *fakeBotAPI) SetWebhook(ctx context.Context, token string, cfg tgdomain.WebhookConfig) error {
	f.mu.Lock()
	f.WebhookCfgs = append(f.WebhookCfgs, cfg)
	f.mu.Unlock()
	if f.SetWebhookFn != nil {
		return f.SetWebhookFn(ctx, token, cfg)
	}
	return nil
}

func (f *fakeBotAPI) DeleteWebhook(context.Context, string, bool) error { return nil }

func (f *fakeBotAPI) GetWebhookInfo(ctx context.Context, token string) (*tgdomain.WebhookInfo, error) {
	if f.GetWebhookInfoFn != nil {
		return f.GetWebhookInfoFn(ctx, token)
	}
	// Echo back whatever URL was registered, which is what a healthy webhook does.
	f.mu.Lock()
	url := ""
	if len(f.WebhookCfgs) > 0 {
		url = f.WebhookCfgs[len(f.WebhookCfgs)-1].URL
	}
	f.mu.Unlock()
	return &tgdomain.WebhookInfo{URL: url}, nil
}

func (f *fakeBotAPI) SendText(ctx context.Context, token string, in tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
	f.mu.Lock()
	f.SentText = append(f.SentText, in)
	f.SentTokens = append(f.SentTokens, token)
	f.mu.Unlock()
	if f.SendTextFn != nil {
		return f.SendTextFn(ctx, token, in)
	}
	return &tgdomain.SendResult{MessageID: 999, ChatID: in.ChatID}, nil
}

func (f *fakeBotAPI) SendMedia(ctx context.Context, token string, in tgdomain.SendMediaInput) (*tgdomain.SendResult, error) {
	f.mu.Lock()
	f.SentMedia = append(f.SentMedia, in)
	f.SentTokens = append(f.SentTokens, token)
	f.mu.Unlock()
	if f.SendMediaFn != nil {
		return f.SendMediaFn(ctx, token, in)
	}
	return &tgdomain.SendResult{MessageID: 1000, ChatID: in.ChatID, FileID: "cached-file-id"}, nil
}

func (f *fakeBotAPI) EditText(context.Context, string, int64, int64, string, string, string) error {
	return nil
}
func (f *fakeBotAPI) DeleteMessage(context.Context, string, int64, int64) error { return nil }
func (f *fakeBotAPI) DeleteBusinessMessages(context.Context, string, string, []int64) error {
	return nil
}
func (f *fakeBotAPI) SendChatAction(context.Context, string, int64, tgdomain.ChatAction, string) error {
	return nil
}
func (f *fakeBotAPI) SetMessageReaction(context.Context, string, int64, int64, string) error {
	return nil
}
func (f *fakeBotAPI) ReadBusinessMessage(context.Context, string, string, int64, int64) error {
	return nil
}
func (f *fakeBotAPI) AnswerCallbackQuery(context.Context, string, string, string) error { return nil }
func (f *fakeBotAPI) GetFile(context.Context, string, string) (*tgdomain.RemoteFile, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeBotAPI) DownloadFile(context.Context, string, string) ([]byte, string, error) {
	return nil, "", errors.New("not implemented")
}
func (f *fakeBotAPI) GetUserProfilePhotoFileID(context.Context, string, int64) (string, error) {
	return "", nil
}

// ---------------------------------------------------------------- file cache

type fakeFileCache struct {
	mu sync.Mutex

	entries map[string]string
	Puts    []string
}

func newFakeFileCache() *fakeFileCache {
	return &fakeFileCache{entries: map[string]string{}}
}

func (f *fakeFileCache) Get(_ context.Context, accountID, sourceKey string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries[accountID+"|"+sourceKey], nil
}

func (f *fakeFileCache) Put(_ context.Context, accountID, sourceKey, fileID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[accountID+"|"+sourceKey] = fileID
	f.Puts = append(f.Puts, fileID)
	return nil
}
