package instagram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
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

func TestSendPrivateReply_ClaimsBeforeSending(t *testing.T) {
	account := connectedAccount()
	messaging := &fakeMessagingService{}
	claims := &fakePrivateReplyRepo{}

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

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "hello"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(order) != 1 || order[0] != "claim" {
		t.Fatalf("claim was not recorded: %v", order)
	}
	if len(messaging.Sent) != 1 {
		t.Fatalf("got %d sends, want 1", len(messaging.Sent))
	}
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

func TestSendPrivateReply_SecondAttemptRefused(t *testing.T) {
	account := connectedAccount()
	messaging := &fakeMessagingService{}
	claims := &fakePrivateReplyRepo{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), messaging,
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "first"); err != nil {
		t.Fatalf("first attempt: %v", err)
	}

	err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "second")
	if !errors.Is(err, igdomain.ErrPrivateReplyUsed) {
		t.Fatalf("second attempt err = %v, want ErrPrivateReplyUsed", err)
	}
	if len(messaging.Sent) != 1 {
		t.Errorf("second attempt reached Instagram; sends = %d, want 1", len(messaging.Sent))
	}
}

func TestSendPrivateReply_FailureKeepsAllowanceConsumed(t *testing.T) {
	account := connectedAccount()
	claims := &fakePrivateReplyRepo{}
	messaging := &fakeMessagingService{
		SendTextFn: func(context.Context, string, string, igdomain.SendTextInput) (*igdomain.SendResult, error) {
			return nil, errors.New("gateway timeout")
		},
	}
	failing := &failingPrivateReplyMessaging{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), failing,
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)
	_ = messaging

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "hi"); err == nil {
		t.Fatal("expected the send to fail")
	}
	if claims.Failed != 1 {
		t.Errorf("MarkFailed calls = %d, want 1", claims.Failed)
	}

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "hi again"); !errors.Is(err, igdomain.ErrPrivateReplyUsed) {
		t.Fatalf("retry err = %v, want ErrPrivateReplyUsed", err)
	}
}

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

	err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "too late")
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

func TestSendPrivateReply_CreatesConversationFromRecipientID(t *testing.T) {
	account := connectedAccount()
	contacts := &fakeContactRepo{}
	convs := &fakeConversationRepo{}

	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, &fakePrivateReplyRepo{},
		contacts, convs,
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "hi"); err != nil {
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
	account.GrantedScopes = []string{igdomain.ScopeBasic, igdomain.ScopeManageMessages}

	claims := &fakePrivateReplyRepo{}
	uc := NewSendPrivateReplyUseCase(
		accountRepoFor(account), &fakeMessagingService{},
		&fakeCommentRepo{}, claims,
		&fakeContactRepo{}, &fakeConversationRepo{},
	)

	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "comment-1", conversation.SentByPerson("user-1"), "hi"); err == nil {
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
	if err := uc.Execute(context.Background(), account.WorkspaceID, account.ID, "c", conversation.SentByPerson("user-1"), tooLong); !errors.Is(err, igdomain.ErrTextTooLong) {
		t.Fatalf("err = %v, want ErrTextTooLong", err)
	}
	if claims.Claims != 0 {
		t.Error("the allowance was claimed for an over-long message")
	}
}

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

func TestAccountResolver_EnforcesWorkspaceOwnership(t *testing.T) {
	account := connectedAccount()
	uc := NewModerateCommentUseCase(accountRepoFor(account), &fakeCommentService{}, &fakeCommentRepo{})

	err := uc.SetHidden(context.Background(), "another-workspace", account.ID, "c1", true)
	if !errors.Is(err, igdomain.ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
}

type failingPrivateReplyMessaging struct {
	fakeMessagingService
}

func (f *failingPrivateReplyMessaging) SendPrivateReply(context.Context, string, string, string, string) (*igdomain.SendResult, error) {
	return nil, errors.New("gateway timeout")
}
