package audience_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	ca "vozko/domain/audience"
)

type fakeSender struct {
	sent []ca.EscalationDelivery
	err  error
}

func (f *fakeSender) Send(_ context.Context, in ca.EscalationDelivery) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, in)
	return nil
}

func escalatableComment(t *testing.T, repo *fakeRepo, ws string) *ca.Analysis {
	t.Helper()
	a, err := ca.NewPending(ca.NewInput{
		WorkspaceID: ws, Container: ref(), SubjectID: "c-1",
		AuthorExternalID: "ig-99", AuthorHandle: "fulano", Text: "vocês são uns ladrões", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ID = "row-1"
	if _, err := repo.Insert(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestEscalateSendsTheFormattedComment(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")
	sender := &fakeSender{}

	uc := NewEscalateCommentUseCase(repo, nil, sender)
	out, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-1", CommentID: "row-1", RecipientID: "contact-7", Note: "vê isso",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d messages", len(sender.sent))
	}
	got := sender.sent[0]
	if got.RecipientID != "contact-7" || got.WorkspaceID != "ws-1" {
		t.Fatalf("delivery = %+v", got)
	}
	for _, want := range []string{"@fulano", "vocês são uns ladrões", "vê isso"} {
		if !strings.Contains(got.Text, want) {
			t.Fatalf("text is missing %q:\n%s", want, got.Text)
		}
	}
	if out == nil || out.Author() != "@fulano" {
		t.Fatalf("escalation = %+v", out)
	}
}

func TestEscalateRefusesAnotherWorkspacesComment(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")
	sender := &fakeSender{}

	uc := NewEscalateCommentUseCase(repo, nil, sender)
	_, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-2", CommentID: "row-1", RecipientID: "contact-7",
	})
	if !errors.Is(err, ca.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(sender.sent) != 0 {
		t.Fatal("nothing may be sent for a comment of another workspace")
	}
}

func TestEscalateRequiresARecipient(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")
	sender := &fakeSender{}

	uc := NewEscalateCommentUseCase(repo, nil, sender)
	_, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-1", CommentID: "row-1", RecipientID: "   ",
	})
	if !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
	if len(sender.sent) != 0 {
		t.Fatal("nothing may be sent without a recipient")
	}
}

func TestEscalateWithoutASenderRefuses(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")

	uc := NewEscalateCommentUseCase(repo, nil, nil)
	_, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-1", CommentID: "row-1", RecipientID: "contact-7",
	})
	if !errors.Is(err, ca.ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
}

func TestEscalateSurfacesASendFailure(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")
	boom := errors.New("channel offline")

	uc := NewEscalateCommentUseCase(repo, nil, &fakeSender{err: boom})
	if _, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-1", CommentID: "row-1", RecipientID: "contact-7",
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the channel's own error", err)
	}
}

func TestEscalateSurvivesAnAdapterThatCannotAnswer(t *testing.T) {
	repo := newFakeRepo()
	escalatableComment(t, repo, "ws-1")
	sender := &fakeSender{}
	adapter := &fakeAdapter{containerErr: errors.New("instagram down")}

	uc := NewEscalateCommentUseCase(repo, map[ca.Source]ca.SourceAdapter{ca.SourceInstagram: adapter}, sender)
	if _, err := uc.Execute(context.Background(), ca.EscalateCommentInput{
		WorkspaceID: "ws-1", CommentID: "row-1", RecipientID: "contact-7",
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatal("the escalation must still go out without a permalink")
	}
	if strings.Contains(sender.sent[0].Text, "Post: \n") {
		t.Fatalf("the post line must fall back to the container id:\n%s", sender.sent[0].Text)
	}
}
