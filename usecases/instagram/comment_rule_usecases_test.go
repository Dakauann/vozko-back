package instagram

import (
	"context"
	"errors"
	"testing"

	ca "vozko/domain/commentautomation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
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

	NewCommentRules(cauc.NewEvaluator(rules), NewCommentActionRunner(nil, nil, moderate)).Execute(context.Background(), inbound("eu quero"))

	if len(rules.asked) != 1 || rules.asked[0] != "instagram|acct-1|m-1" {
		t.Fatalf("candidates asked = %v", rules.asked)
	}
	if len(comments.HiddenTo) != 1 || !comments.HiddenTo[0] {
		t.Fatalf("hidden = %v", comments.HiddenTo)
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
