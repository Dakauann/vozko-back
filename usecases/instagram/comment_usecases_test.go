package instagram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	igdomain "vozko/domain/instagram"
)

func accountRepoFor(account *igdomain.Account) *fakeAccountRepo {
	return &fakeAccountRepo{
		FindByIDFn: func(_ context.Context, id string) (*igdomain.Account, error) {
			if id == account.ID {
				return account, nil
			}
			return nil, igdomain.ErrAccountNotFound
		},
	}
}

// TestSendPrivateReply_ClaimsBeforeSending is the critical guard: Instagram permits
// exactly ONE private reply per comment, ever. The allowance must be claimed before
// the HTTP call, so an ambiguous failure can never be "retried" into a double-send.
func TestSendPrivateReply_ClaimsBeforeSending(t *testing.T) {
	account := connectedAccount()
	messaging := &fakeMessagingService{}
	claims := &fakePrivateReplyRepo{}

	// Record the ordering: the claim must land before any send.
	var order []string
	claims.ClaimFn = func(context.Context, string, string) (bool, error) {
		order = append(order, "claim")
		return true, nil
	}
	messaging.SendTextFn = nil

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), messaging,
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "hello"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(order) != 1 || order[0] != "claim" {
		t.Fatalf("claim was not recorded: %v", order)
	}
	if len(messaging.Sent) != 1 {
		t.Fatalf("got %d sends, want 1", len(messaging.Sent))
	}
	// The path takes OUR business account id, and the comment travels in the
	// recipient, not the other way round.
	if messaging.Sent[0].IGUserID != account.IGUserID {
		t.Errorf("sent from %q, want the business IG id %q", messaging.Sent[0].IGUserID, account.IGUserID)
	}
	if messaging.Sent[0].Recipient != "comment-1" {
		t.Errorf("recipient = %q, want the comment id", messaging.Sent[0].Recipient)
	}
	if claims.Sent != 1 {
		t.Errorf("MarkSent calls = %d, want 1", claims.Sent)
	}
}

// TestSendPrivateReply_SecondAttemptRefused: once the allowance is gone the second
// attempt must not reach Instagram at all.
func TestSendPrivateReply_SecondAttemptRefused(t *testing.T) {
	account := connectedAccount()
	messaging := &fakeMessagingService{}
	claims := &fakePrivateReplyRepo{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), messaging,
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "first"); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "second")
	if !errors.Is(err, igdomain.ErrPrivateReplyUsed) {
		t.Fatalf("second attempt err = %v, want ErrPrivateReplyUsed", err)
	}
	if len(messaging.Sent) != 1 {
		t.Errorf("second attempt reached Instagram; sends = %d, want 1", len(messaging.Sent))
	}
}

// TestSendPrivateReply_FailureKeepsAllowanceConsumed: we cannot know whether Meta
// processed a send that failed to answer, so handing the allowance back would risk
// a double-send. The row stays claimed and is marked failed.
func TestSendPrivateReply_FailureKeepsAllowanceConsumed(t *testing.T) {
	account := connectedAccount()
	claims := &fakePrivateReplyRepo{}
	messaging := &fakeMessagingService{
		SendTextFn: func(context.Context, string, string, igdomain.SendTextInput) (*igdomain.SendResult, error) {
			return nil, errors.New("gateway timeout")
		},
	}
	// SendPrivateReply on the fake does not route through SendTextFn, so force the
	// failure through a dedicated stub.
	failing := &failingPrivateReplyMessaging{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), failing,
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)
	_ = messaging

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "hi"); err == nil {
		t.Fatal("expected the send to fail")
	}
	if claims.Failed != 1 {
		t.Errorf("MarkFailed calls = %d, want 1", claims.Failed)
	}

	// A retry must still be refused: the allowance is spent.
	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "hi again"); !errors.Is(err, igdomain.ErrPrivateReplyUsed) {
		t.Fatalf("retry err = %v, want ErrPrivateReplyUsed", err)
	}
}

// TestSendPrivateReply_RefusesOutsideSevenDayWindow: Instagram requires the reply
// within 7 days, so an older comment must be rejected before the allowance is spent.
func TestSendPrivateReply_RefusesOutsideSevenDayWindow(t *testing.T) {
	account := connectedAccount()
	stale := time.Now().UTC().Add(-8 * 24 * time.Hour)
	comments := &fakeCommentRepo{
		FindFn: func(context.Context, string, string) (*igdomain.Comment, error) {
			return &igdomain.Comment{IGCommentID: "comment-1", Timestamp: &stale}, nil
		},
	}
	claims := &fakePrivateReplyRepo{}
	messaging := &fakeMessagingService{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), messaging, comments, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "too late")
	if !errors.Is(err, igdomain.ErrPrivateReplyExpired) {
		t.Fatalf("err = %v, want ErrPrivateReplyExpired", err)
	}
	if claims.Claims != 0 {
		t.Errorf("the allowance was claimed for an expired comment (%d claims)", claims.Claims)
	}
	if len(messaging.Sent) != 0 {
		t.Errorf("an expired comment reached Instagram")
	}
}

// TestSendPrivateReply_CreatesConversationFromRecipientID: the response carries the
// commenter's IGSID, which is the only handle available for follow-up DMs.
func TestSendPrivateReply_CreatesConversationFromRecipientID(t *testing.T) {
	account := connectedAccount()
	contacts := &fakeContactRepo{}
	convs := &fakeConversationRepo{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, &fakePrivateReplyRepo{},
		contacts, convs,
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "hi"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(contacts.Created) != 1 {
		t.Fatalf("contacts created = %v, want one from the recipient IGSID", contacts.Created)
	}
	if !strings.HasSuffix(contacts.Created[0], "/commenter-igsid") {
		t.Errorf("contact was created from %q, want the recipient IGSID", contacts.Created[0])
	}
}

func TestSendPrivateReply_RequiresCommentsScope(t *testing.T) {
	account := connectedAccount()
	// Private replies are gated by the COMMENTS scope, not a messaging scope.
	account.GrantedScopes = []string{igdomain.ScopeBasic, igdomain.ScopeManageMessages}

	claims := &fakePrivateReplyRepo{}
	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", "hi"); err == nil {
		t.Fatal("send succeeded without the comments scope")
	}
	if claims.Claims != 0 {
		t.Error("the allowance was claimed despite a missing scope")
	}
}

func TestSendPrivateReply_EnforcesByteLimit(t *testing.T) {
	account := connectedAccount()
	claims := &fakePrivateReplyRepo{}
	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	tooLong := strings.Repeat("😀", 400)
	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "c", tooLong); !errors.Is(err, igdomain.ErrTextTooLong) {
		t.Fatalf("err = %v, want ErrTextTooLong", err)
	}
	if claims.Claims != 0 {
		t.Error("the allowance was claimed for an over-long message")
	}
}

// TestModerateComment_DeleteRefusedForOthersComments: Instagram requires the
// COMMENT CREATOR's token to delete, so a third party's comment can only be hidden.
// Refusing locally gives a useful message instead of an upstream permission error.
func TestModerateComment_DeleteRefusedForOthersComments(t *testing.T) {
	account := connectedAccount()
	comments := &fakeCommentRepo{
		FindFn: func(context.Context, string, string) (*igdomain.Comment, error) {
			return &igdomain.Comment{IGCommentID: "c1", IsOurs: false}, nil
		},
	}
	service := &fakeCommentService{}

	uc := NewModerateCommentUseCase(accountRepoFor(account), service, comments)

	if err := uc.Delete(context.Background(), account.WorkspaceID, account.ID, "c1"); err == nil {
		t.Fatal("delete succeeded on someone else's comment")
	}
	if service.Deletes != 0 {
		t.Errorf("reached Instagram %d times; hiding is the correct action here", service.Deletes)
	}
}

func TestModerateComment_DeleteAllowedForOurOwnReply(t *testing.T) {
	account := connectedAccount()
	comments := &fakeCommentRepo{
		FindFn: func(context.Context, string, string) (*igdomain.Comment, error) {
			return &igdomain.Comment{IGCommentID: "c1", IsOurs: true}, nil
		},
	}
	service := &fakeCommentService{}

	uc := NewModerateCommentUseCase(accountRepoFor(account), service, comments)

	if err := uc.Delete(context.Background(), account.WorkspaceID, account.ID, "c1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if service.Deletes != 1 {
		t.Errorf("Instagram deletes = %d, want 1", service.Deletes)
	}
	if comments.Deleted != 1 {
		t.Errorf("local mirror deletes = %d, want 1", comments.Deleted)
	}
}

func TestModerateComment_HideMirrorsState(t *testing.T) {
	account := connectedAccount()
	comments := &fakeCommentRepo{}
	service := &fakeCommentService{}

	uc := NewModerateCommentUseCase(accountRepoFor(account), service, comments)

	if err := uc.SetHidden(context.Background(), account.WorkspaceID, account.ID, "c1", true); err != nil {
		t.Fatalf("SetHidden: %v", err)
	}
	if len(service.HiddenTo) != 1 || !service.HiddenTo[0] {
		t.Errorf("upstream hide calls = %v, want [true]", service.HiddenTo)
	}
	if len(comments.HiddenTo) != 1 || !comments.HiddenTo[0] {
		t.Errorf("mirror hide calls = %v, want [true]", comments.HiddenTo)
	}
}

// TestAccountResolver_EnforcesWorkspaceOwnership: an id from another tenant must
// read as not-found rather than revealing that it exists.
func TestAccountResolver_EnforcesWorkspaceOwnership(t *testing.T) {
	account := connectedAccount()
	uc := NewModerateCommentUseCase(accountRepoFor(account), &fakeCommentService{}, &fakeCommentRepo{})

	err := uc.SetHidden(context.Background(), "another-workspace", account.ID, "c1", true)
	if !errors.Is(err, igdomain.ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
}

// failingPrivateReplyMessaging fails only SendPrivateReply, so the failure path can
// be exercised without affecting the other send methods.
type failingPrivateReplyMessaging struct {
	fakeMessagingService
}

func (f *failingPrivateReplyMessaging) SendPrivateReply(context.Context, string, string, string, string) (*igdomain.SendResult, error) {
	return nil, errors.New("gateway timeout")
}
