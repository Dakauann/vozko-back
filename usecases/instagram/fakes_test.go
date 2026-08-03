package instagram

import (
	"context"
	"time"

	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

// Hand-written fakes with function fields, matching the repository's dominant test
// style. Each defaults to a benign value so a test only sets what it exercises.

type fakeAccountRepo struct {
	FindByIDFn               func(ctx context.Context, id string) (*igdomain.Account, error)
	FindByIGUserIDFn         func(ctx context.Context, igUserID string) (*igdomain.Account, error)
	FindByIGUserIDUnscopedFn func(ctx context.Context, igUserID string) (*igdomain.Account, error)
	UpdateStatusFn           func(ctx context.Context, id string, status igdomain.Status, reason string) error
	UpdateTokenFn            func(ctx context.Context, id, token string, expiresAt, refreshedAt time.Time) error
	ListDueFn                func(ctx context.Context, before time.Time, limit int) ([]*igdomain.Account, error)

	StatusUpdates []igdomain.Status
	TokenUpdates  int
}

func (f *fakeAccountRepo) Create(context.Context, *igdomain.Account) error { return nil }
func (f *fakeAccountRepo) Update(context.Context, *igdomain.Account) error { return nil }

func (f *fakeAccountRepo) UpdateToken(ctx context.Context, id, token string, expiresAt, refreshedAt time.Time) error {
	f.TokenUpdates++
	if f.UpdateTokenFn != nil {
		return f.UpdateTokenFn(ctx, id, token, expiresAt, refreshedAt)
	}
	return nil
}

func (f *fakeAccountRepo) UpdateStatus(ctx context.Context, id string, status igdomain.Status, reason string) error {
	f.StatusUpdates = append(f.StatusUpdates, status)
	if f.UpdateStatusFn != nil {
		return f.UpdateStatusFn(ctx, id, status, reason)
	}
	return nil
}

func (f *fakeAccountRepo) UpdateMessagingHealth(context.Context, string, bool, time.Time) error {
	return nil
}
func (f *fakeAccountRepo) SetWebhookSubscribedAt(context.Context, string, time.Time) error {
	return nil
}

func (f *fakeAccountRepo) FindByID(ctx context.Context, id string) (*igdomain.Account, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, igdomain.ErrAccountNotFound
}

func (f *fakeAccountRepo) FindByIGUserID(ctx context.Context, igUserID string) (*igdomain.Account, error) {
	if f.FindByIGUserIDFn != nil {
		return f.FindByIGUserIDFn(ctx, igUserID)
	}
	return nil, igdomain.ErrAccountNotFound
}

func (f *fakeAccountRepo) FindByIGUserIDUnscoped(ctx context.Context, igUserID string) (*igdomain.Account, error) {
	if f.FindByIGUserIDUnscopedFn != nil {
		return f.FindByIGUserIDUnscopedFn(ctx, igUserID)
	}
	return nil, igdomain.ErrAccountNotFound
}

func (f *fakeAccountRepo) Restore(context.Context, string) error { return nil }

func (f *fakeAccountRepo) ListByWorkspace(context.Context, igdomain.ListAccountsInput) (*shared.PaginatedResult[*igdomain.Account], error) {
	return shared.NewPaginatedResult([]*igdomain.Account{}, shared.Pagination{}, 0), nil
}

func (f *fakeAccountRepo) ListDueForTokenRefresh(ctx context.Context, before time.Time, limit int) ([]*igdomain.Account, error) {
	if f.ListDueFn != nil {
		return f.ListDueFn(ctx, before, limit)
	}
	return nil, nil
}

func (f *fakeAccountRepo) Delete(context.Context, string) error { return nil }

type fakeContactRepo struct {
	FindByIDFn     func(ctx context.Context, id string) (*igdomain.Contact, error)
	FindOrCreateFn func(ctx context.Context, workspaceID, igAccountID, igsid string) (*igdomain.Contact, error)

	Created []string
}

func (f *fakeContactRepo) FindOrCreate(ctx context.Context, workspaceID, igAccountID, igsid string) (*igdomain.Contact, error) {
	f.Created = append(f.Created, igAccountID+"/"+igsid)
	if f.FindOrCreateFn != nil {
		return f.FindOrCreateFn(ctx, workspaceID, igAccountID, igsid)
	}
	return &igdomain.Contact{
		ID:          "contact-" + igsid,
		WorkspaceID: workspaceID,
		IGAccountID: igAccountID,
		IGSID:       igsid,
	}, nil
}

func (f *fakeContactRepo) FindByID(ctx context.Context, id string) (*igdomain.Contact, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, igdomain.ErrContactNotFound
}

func (f *fakeContactRepo) FindByIDs(ctx context.Context, ids []string) ([]*igdomain.Contact, error) {
	if f.FindByIDFn == nil {
		return nil, nil
	}
	out := make([]*igdomain.Contact, 0, len(ids))
	for _, id := range ids {
		c, err := f.FindByIDFn(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeContactRepo) FindByIGSID(context.Context, string, string) (*igdomain.Contact, error) {
	return nil, igdomain.ErrContactNotFound
}
func (f *fakeContactRepo) UpdateProfile(context.Context, string, igdomain.ContactProfile) error {
	return nil
}
func (f *fakeContactRepo) SetBlocked(context.Context, string, bool) error { return nil }

type fakeConversationRepo struct {
	FindByIDFn     func(ctx context.Context, id string) (*igdomain.Conversation, error)
	FindOrCreateFn func(ctx context.Context, workspaceID, igAccountID, contactID string) (*igdomain.Conversation, error)

	InboundRecorded  int
	OutboundRecorded int
}

func (f *fakeConversationRepo) FindOrCreate(ctx context.Context, workspaceID, igAccountID, contactID string) (*igdomain.Conversation, error) {
	if f.FindOrCreateFn != nil {
		return f.FindOrCreateFn(ctx, workspaceID, igAccountID, contactID)
	}
	return &igdomain.Conversation{
		ID:          "conv-1",
		WorkspaceID: workspaceID,
		IGAccountID: igAccountID,
		ContactID:   contactID,
	}, nil
}

func (f *fakeConversationRepo) FindByID(ctx context.Context, id string) (*igdomain.Conversation, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, id)
	}
	return nil, igdomain.ErrConversationNotFound
}

func (f *fakeConversationRepo) FindByContact(context.Context, string, string) (*igdomain.Conversation, error) {
	return nil, igdomain.ErrConversationNotFound
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

func (f *fakeConversationRepo) RecordInbound(context.Context, string, time.Time) error {
	f.InboundRecorded++
	return nil
}

func (f *fakeConversationRepo) RecordOutbound(context.Context, string, time.Time) error {
	f.OutboundRecorded++
	return nil
}

func (f *fakeConversationRepo) SetIGConversationID(context.Context, string, string) error { return nil }
func (f *fakeConversationRepo) StatusForEntry(context.Context, string) (string, error) {
	return "", nil
}

func (f *fakeConversationRepo) CountByStatus(context.Context, string, string) (map[string]int64, error) {
	return nil, nil
}

func (f *fakeConversationRepo) SetAutomationEnabled(context.Context, string, *bool) error {
	return nil
}

func (f *fakeConversationRepo) SetStatus(context.Context, string, string, string, string, *time.Time) error {
	return nil
}

// sentMessage records one outbound Send API call so a test can assert WHICH
// account and token were used, the property that keeps multi-account replies
// correct.
type sentMessage struct {
	IGUserID  string
	Token     string
	Recipient string
	Text      string
	Kind      string
	URL       string
	ReplyTo   string
}

type fakeMessagingService struct {
	SendTextFn  func(ctx context.Context, igUserID, token string, in igdomain.SendTextInput) (*igdomain.SendResult, error)
	SendMediaFn func(ctx context.Context, igUserID, token string, in igdomain.SendMediaInput) (*igdomain.SendResult, error)

	Sent []sentMessage
}

func (f *fakeMessagingService) SendText(ctx context.Context, igUserID, token string, in igdomain.SendTextInput) (*igdomain.SendResult, error) {
	f.Sent = append(f.Sent, sentMessage{
		IGUserID: igUserID, Token: token,
		Recipient: in.RecipientIGSID, Text: in.Text, ReplyTo: in.ReplyToMID,
	})
	if f.SendTextFn != nil {
		return f.SendTextFn(ctx, igUserID, token, in)
	}
	return &igdomain.SendResult{RecipientID: in.RecipientIGSID, MessageID: "mid-sent"}, nil
}

func (f *fakeMessagingService) SendMedia(ctx context.Context, igUserID, token string, in igdomain.SendMediaInput) (*igdomain.SendResult, error) {
	f.Sent = append(f.Sent, sentMessage{
		IGUserID: igUserID, Token: token,
		Recipient: in.RecipientIGSID, Kind: in.Kind, URL: in.URL, ReplyTo: in.ReplyToMID,
	})
	if f.SendMediaFn != nil {
		return f.SendMediaFn(ctx, igUserID, token, in)
	}
	return &igdomain.SendResult{RecipientID: in.RecipientIGSID, MessageID: "mid-media"}, nil
}

func (f *fakeMessagingService) SendReaction(context.Context, string, string, string, string, string) error {
	return nil
}
func (f *fakeMessagingService) RemoveReaction(context.Context, string, string, string, string) error {
	return nil
}
func (f *fakeMessagingService) SendTyping(context.Context, string, string, string, bool) error {
	return nil
}
func (f *fakeMessagingService) MarkSeen(context.Context, string, string, string) error { return nil }

func (f *fakeMessagingService) SendPrivateReply(ctx context.Context, igUserID, token, igCommentID, text string) (*igdomain.SendResult, error) {
	f.Sent = append(f.Sent, sentMessage{
		IGUserID: igUserID, Token: token, Recipient: igCommentID, Text: text,
	})
	return &igdomain.SendResult{RecipientID: "commenter-igsid", MessageID: "mid-private"}, nil
}

func (f *fakeMessagingService) GetContactProfile(context.Context, string, string) (*igdomain.ContactProfileResult, error) {
	return &igdomain.ContactProfileResult{Username: "someone"}, nil
}

func (f *fakeMessagingService) GetConversations(context.Context, string, string, int) error {
	return nil
}

type fakeOAuthService struct {
	RefreshFn func(ctx context.Context, token string) (*igdomain.TokenGrant, error)
	Refreshes int
}

func (f *fakeOAuthService) BuildAuthorizeURL(state string) string {
	return "https://www.instagram.com/oauth/authorize?state=" + state
}

func (f *fakeOAuthService) ExchangeCode(context.Context, string) (*igdomain.TokenGrant, error) {
	return &igdomain.TokenGrant{AccessToken: "short", UserID: "ig-1"}, nil
}

func (f *fakeOAuthService) ExchangeForLongLived(context.Context, string) (*igdomain.TokenGrant, error) {
	return &igdomain.TokenGrant{AccessToken: "long", ExpiresIn: 60 * 24 * time.Hour}, nil
}

func (f *fakeOAuthService) RefreshToken(ctx context.Context, token string) (*igdomain.TokenGrant, error) {
	f.Refreshes++
	if f.RefreshFn != nil {
		return f.RefreshFn(ctx, token)
	}
	return &igdomain.TokenGrant{AccessToken: "refreshed", ExpiresIn: 60 * 24 * time.Hour}, nil
}

func (f *fakeOAuthService) GetProfile(context.Context, string) (*igdomain.Profile, error) {
	return &igdomain.Profile{IGUserID: "ig-1", Username: "acct"}, nil
}

type fakePrivateReplyRepo struct {
	ClaimFn func(ctx context.Context, igCommentID, igAccountID string) (bool, error)

	Claims  int
	Sent    int
	Failed  int
	claimed map[string]bool
}

func (f *fakePrivateReplyRepo) Claim(ctx context.Context, igCommentID, igAccountID string) (bool, error) {
	f.Claims++
	if f.ClaimFn != nil {
		return f.ClaimFn(ctx, igCommentID, igAccountID)
	}
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	if f.claimed[igCommentID] {
		return false, nil
	}
	f.claimed[igCommentID] = true
	return true, nil
}

func (f *fakePrivateReplyRepo) MarkSent(context.Context, string, string, string) error {
	f.Sent++
	return nil
}

func (f *fakePrivateReplyRepo) MarkFailed(context.Context, string, int, string) error {
	f.Failed++
	return nil
}

func (f *fakePrivateReplyRepo) Find(context.Context, string) (*igdomain.PrivateReply, error) {
	return nil, nil
}

type fakeCommentRepo struct {
	FindFn func(ctx context.Context, igAccountID, igCommentID string) (*igdomain.Comment, error)

	Upserts  int
	Deleted  int
	HiddenTo []bool
}

func (f *fakeCommentRepo) Upsert(context.Context, *igdomain.Comment) error {
	f.Upserts++
	return nil
}

func (f *fakeCommentRepo) UpsertMany(_ context.Context, items []*igdomain.Comment) error {
	f.Upserts += len(items)
	return nil
}

func (f *fakeCommentRepo) FindByIGCommentID(ctx context.Context, igAccountID, igCommentID string) (*igdomain.Comment, error) {
	if f.FindFn != nil {
		return f.FindFn(ctx, igAccountID, igCommentID)
	}
	return nil, igdomain.ErrCommentNotFound
}

func (f *fakeCommentRepo) SetHidden(_ context.Context, _, _ string, hidden bool) error {
	f.HiddenTo = append(f.HiddenTo, hidden)
	return nil
}

func (f *fakeCommentRepo) Delete(context.Context, string, string) error {
	f.Deleted++
	return nil
}

func (f *fakeCommentRepo) ListByMedia(context.Context, igdomain.ListCommentsInput) (*shared.PaginatedResult[*igdomain.Comment], error) {
	return shared.NewPaginatedResult([]*igdomain.Comment{}, shared.Pagination{}, 0), nil
}

type fakeCommentService struct {
	ListFn   func(ctx context.Context, token, igMediaID string, limit int, after string) (*igdomain.Page[*igdomain.RemoteComment], error)
	DeleteFn func(ctx context.Context, token, igCommentID string) error

	Deletes  int
	HiddenTo []bool
}

func (f *fakeCommentService) SetHidden(_ context.Context, _, _ string, hidden bool) error {
	f.HiddenTo = append(f.HiddenTo, hidden)
	return nil
}

func (f *fakeCommentService) ListComments(ctx context.Context, token, igMediaID string, limit int, after string) (*igdomain.Page[*igdomain.RemoteComment], error) {
	if f.ListFn != nil {
		return f.ListFn(ctx, token, igMediaID, limit, after)
	}
	return &igdomain.Page[*igdomain.RemoteComment]{}, nil
}

func (f *fakeCommentService) GetComment(context.Context, string, string) (*igdomain.RemoteComment, error) {
	return nil, igdomain.ErrCommentNotFound
}

func (f *fakeCommentService) ListReplies(context.Context, string, string, int, string) (*igdomain.Page[*igdomain.RemoteComment], error) {
	return &igdomain.Page[*igdomain.RemoteComment]{}, nil
}

func (f *fakeCommentService) ReplyToComment(context.Context, string, string, string) (string, error) {
	return "new-comment", nil
}

func (f *fakeCommentService) CreateComment(context.Context, string, string, string) (string, error) {
	return "new-comment", nil
}

func (f *fakeCommentService) Delete(ctx context.Context, token, igCommentID string) error {
	f.Deletes++
	if f.DeleteFn != nil {
		return f.DeleteFn(ctx, token, igCommentID)
	}
	return nil
}

// connectedAccount is a ready-to-send account with every relevant scope granted.
func connectedAccount() *igdomain.Account {
	return &igdomain.Account{
		ID:          "acct-1",
		WorkspaceID: "ws-1",
		IGUserID:    "17841400000000001",
		Username:    "brand",
		AccessToken: "token-acct-1",
		Status:      igdomain.StatusConnected,
		GrantedScopes: []string{
			igdomain.ScopeBasic,
			igdomain.ScopeManageMessages,
			igdomain.ScopeManageComments,
			igdomain.ScopeContentPublish,
		},
	}
}
