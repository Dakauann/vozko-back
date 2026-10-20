package instagram

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/commentautomation"
	igdomain "vozko/domain/instagram"
	mm "vozko/domain/metamessaging"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
	webhook_usecase "vozko/usecases/webhook"
)

type candidateRules struct {
	ca.Repository
	rules []*ca.Rule
	asked []string
}

func (c *candidateRules) ListCandidates(_ context.Context, source shared.EntryType, accountID, containerID string) ([]*ca.Rule, error) {
	c.asked = append(c.asked, string(source)+"|"+accountID+"|"+containerID)
	return c.rules, nil
}

func inbound(text string) *igdomain.Comment {
	return &igdomain.Comment{
		IGCommentID: "c-1", IGMediaID: "m-1",
		IGAccountID: "acct-1", WorkspaceID: "ws-1",
		FromUsername: "maria", Text: text,
	}
}

func TestCommentSubjectCarriesWhatRulesMatchOn(t *testing.T) {
	c := inbound("quero")
	c.Hidden, c.IsOurs = true, true
	got := CommentSubject(c)
	if got.ContainerID != "m-1" || got.AuthorName != "maria" || got.Text != "quero" || !got.Hidden || !got.IsOurs {
		t.Fatalf("subject = %+v", got)
	}
}

func TestInstagramRulesRunThroughTheSharedEvaluator(t *testing.T) {
	hide := &ca.Rule{ID: "r1", WorkspaceID: "ws-1", Source: shared.EntryTypeInstagram, AccountID: "acct-1",
		Name: "Ocultar", Enabled: true, Match: ca.MatchContains, Keywords: []string{"quero"}, Actions: []ca.Action{ca.ActionHide}}
	hide.Normalize()
	rules := &candidateRules{rules: []*ca.Rule{hide}}
	comments := &fakeCommentService{}
	moderate := NewModerateCommentUseCase(accountRepoFor(connectedAccount()), comments, &fakeCommentRepo{})

	err := NewCommentRules(cauc.NewEvaluator(rules, &onceClaims{}), NewCommentActionRunner(nil, nil, moderate)).Execute(context.Background(), inbound("eu quero"))
	if err != nil {
		t.Fatal(err)
	}

	if len(rules.asked) != 1 || rules.asked[0] != "instagram|acct-1|m-1" {
		t.Fatalf("candidates asked = %v", rules.asked)
	}
	if len(comments.HiddenTo) != 1 || !comments.HiddenTo[0] {
		t.Fatalf("hidden = %v", comments.HiddenTo)
	}
}

type onceClaims struct{ taken map[string]bool }

func (o *onceClaims) Claim(_ context.Context, source shared.EntryType, commentID, _ string) (bool, error) {
	if o.taken == nil {
		o.taken = map[string]bool{}
	}
	key := string(source) + "|" + commentID
	if o.taken[key] {
		return false, nil
	}
	o.taken[key] = true
	return true, nil
}

func (o *onceClaims) PurgeOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

type unavailableRules struct{ ca.Repository }

func (unavailableRules) ListCandidates(context.Context, shared.EntryType, string, string) ([]*ca.Rule, error) {
	return nil, errors.New("rules db timeout")
}

func commentEvent() (*igdomain.Account, *igdomain.Event) {
	account := &igdomain.Account{ID: "acct-1", WorkspaceID: "ws-1", IGUserID: "ig-me"}
	ev := &igdomain.Event{
		Event: mm.Event{Kind: igdomain.EventComment, Timestamp: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)},
		Comment: &igdomain.CommentValue{ID: "c-1", Text: "eu quero", From: &igdomain.CommentFrom{ID: "igsid-9", Username: "ana"},
			Media: &igdomain.CommentMedia{ID: "m-1"}},
	}
	return account, ev
}

func TestUnavailableRulesFailTheCommentSoTheConsumerRetries(t *testing.T) {
	enq := &recordingEnqueuer{}
	uc := NewHandleWebhookUseCase(HandleWebhookDeps{
		Comments:     &fakeCommentRepo{},
		CommentRules: NewCommentRules(cauc.NewEvaluator(unavailableRules{}, &onceClaims{}), NewCommentActionRunner(nil, nil, nil)),
		Audience:     enq,
	})
	account, ev := commentEvent()

	err := uc.handleComment(context.Background(), account, ev)

	if !errors.Is(err, ca.ErrRulesUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if classifyWebhookFailure(err) != webhook_usecase.DispositionRetry {
		t.Fatal("unavailable rules must be retried")
	}
}

func TestAFailedRuleActionDoesNotFailTheComment(t *testing.T) {
	hide := &ca.Rule{ID: "r1", WorkspaceID: "ws-1", Source: shared.EntryTypeInstagram, AccountID: "acct-gone",
		Name: "Ocultar", Enabled: true, Match: ca.MatchContains, Keywords: []string{"quero"}, Actions: []ca.Action{ca.ActionHide}}
	hide.Normalize()
	moderate := NewModerateCommentUseCase(accountRepoFor(connectedAccount()), &fakeCommentService{}, &fakeCommentRepo{})
	enq := &recordingEnqueuer{}
	uc := NewHandleWebhookUseCase(HandleWebhookDeps{
		Comments:     &fakeCommentRepo{},
		CommentRules: NewCommentRules(cauc.NewEvaluator(&candidateRules{rules: []*ca.Rule{hide}}, &onceClaims{}), NewCommentActionRunner(nil, nil, moderate)),
		Audience:     enq,
	})
	account, ev := commentEvent()

	if err := uc.handleComment(context.Background(), account, ev); err != nil {
		t.Fatalf("a failed action after the claim must not trigger a pointless retry: %v", err)
	}
	if len(enq.enqueued) != 1 {
		t.Fatalf("the comment still reaches the audience analysis: %v", enq.enqueued)
	}
}

func TestInstagramRefusesFacebookOnlyActions(t *testing.T) {
	runner := NewCommentActionRunner(nil, nil, nil)
	if err := runner.Delete(context.Background(), &ca.Rule{}, "c"); !errors.Is(err, ca.ErrActionUnsupported) {
		t.Fatalf("delete: %v", err)
	}
	if err := runner.Like(context.Background(), &ca.Rule{}, "c"); !errors.Is(err, ca.ErrActionUnsupported) {
		t.Fatalf("like: %v", err)
	}
}

func TestAccountOwnershipStaysInTheWorkspace(t *testing.T) {
	owner := NewAccountOwnership(accountRepoFor(connectedAccount()))
	if err := owner.VerifyAccount(context.Background(), "ws-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	if err := owner.VerifyAccount(context.Background(), "ws-2", "acct-1"); !errors.Is(err, igdomain.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}
