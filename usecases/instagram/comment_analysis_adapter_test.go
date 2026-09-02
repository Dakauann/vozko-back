package instagram

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/comment_analysis"
	igdomain "vozko/domain/instagram"
)

// The adapter is the only file that knows both the Instagram tables and the
// engine's ports. These pin the translation in both directions.

type captureIngestor struct {
	in  []ca.IngestInput
	err error
}

func (c *captureIngestor) Enqueue(_ context.Context, in ca.IngestInput) error {
	c.in = append(c.in, in)
	return c.err
}

type fakeCommentTexts struct {
	fakeCommentRepo
	byID    map[string]*igdomain.Comment
	deleted []string
}

func (f *fakeCommentTexts) FindByIGCommentID(_ context.Context, _, id string) (*igdomain.Comment, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, igdomain.ErrCommentNotFound
}

type fakeAnalysisTombstones struct{ ids []string }

func (f *fakeAnalysisTombstones) SoftDeleteBySourceComment(_ context.Context, _ ca.Source, id string, _ time.Time) error {
	f.ids = append(f.ids, id)
	return nil
}

func caAdapterFixture() (*CommentAnalysisAdapter, *captureIngestor, *fakeCommentTexts, *fakeAnalysisTombstones) {
	ingestor := &captureIngestor{}
	comments := &fakeCommentTexts{byID: map[string]*igdomain.Comment{}}
	tomb := &fakeAnalysisTombstones{}
	media := &fakeMediaRepo{}
	a := NewCommentAnalysisAdapter(ingestor, comments, media, nil, nil, tomb)
	return a, ingestor, comments, tomb
}

func TestAdapter_EnqueueTranslatesTheComment(t *testing.T) {
	a, ingestor, _, _ := caAdapterFixture()
	ts := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	parent := "c-parent"
	a.Enqueue(context.Background(), &igdomain.Comment{
		WorkspaceID: "ws-1", IGAccountID: "acc-1", IGCommentID: "c-1", IGMediaID: "m-1",
		ParentIGCommentID: &parent, FromIGSID: "igsid-1", FromUsername: "maria", Text: "oi", Timestamp: &ts, IsOurs: true,
	})
	if len(ingestor.in) != 1 {
		t.Fatalf("enqueued %d", len(ingestor.in))
	}
	in := ingestor.in[0]
	if in.WorkspaceID != "ws-1" || in.Container != (ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "m-1"}) ||
		in.SourceCommentID != "c-1" || in.ParentCommentID != "c-parent" || in.AuthorExternalID != "igsid-1" ||
		in.AuthorHandle != "maria" || in.Text != "oi" || !in.CommentedAt.Equal(ts) || !in.IsOurs {
		t.Fatalf("translated = %+v", in)
	}
}

// A comment without a post or an id cannot be a container ref; the adapter
// drops it rather than letting the engine reject it per call.
func TestAdapter_EnqueueSkipsIncomplete(t *testing.T) {
	a, ingestor, _, _ := caAdapterFixture()
	a.Enqueue(context.Background(), &igdomain.Comment{WorkspaceID: "ws-1", IGAccountID: "acc-1", IGCommentID: "c-1"})
	a.Enqueue(context.Background(), nil)
	if len(ingestor.in) != 0 {
		t.Fatalf("enqueued %d, want 0", len(ingestor.in))
	}
}

func TestAdapter_ReadTextsSkipsMissing(t *testing.T) {
	a, _, comments, _ := caAdapterFixture()
	comments.byID["c-1"] = &igdomain.Comment{IGCommentID: "c-1", Text: "um"}
	comments.byID["c-3"] = &igdomain.Comment{IGCommentID: "c-3", Text: "três"}
	texts, err := a.ReadTexts(context.Background(), ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "m-1"}, []string{"c-1", "c-2", "c-3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || texts["c-1"] != "um" || texts["c-3"] != "três" {
		t.Fatalf("texts = %v", texts)
	}
}

func TestAdapter_ForgetTombstones(t *testing.T) {
	a, _, _, tomb := caAdapterFixture()
	a.Forget(context.Background(), "c-9")
	if len(tomb.ids) != 1 || tomb.ids[0] != "c-9" {
		t.Fatalf("tombstoned %v", tomb.ids)
	}
}

func TestAdapter_ReadContainerContextUsesTheCaption(t *testing.T) {
	ingestor := &captureIngestor{}
	comments := &fakeCommentTexts{byID: map[string]*igdomain.Comment{}}
	media := &fakeMediaRepo{FindByIGMediaIDFn: func(_ context.Context, acc, id string) (*igdomain.Media, error) {
		if id != "m-1" {
			return nil, igdomain.ErrMediaNotFound
		}
		return &igdomain.Media{IGMediaID: "m-1", Caption: "Asfalto novo", Permalink: "https://ig/p/1"}, nil
	}}
	a := NewCommentAnalysisAdapter(ingestor, comments, media, nil, nil, &fakeAnalysisTombstones{})
	c, err := a.ReadContainerContext(context.Background(), ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "m-1"})
	if err != nil || c.Caption != "Asfalto novo" || c.Permalink != "https://ig/p/1" {
		t.Fatalf("context = %+v %v", c, err)
	}
	// An unknown post is an empty context, not an error: the engine
	// classifies without the caption.
	c, err = a.ReadContainerContext(context.Background(), ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "acc-1", ContainerID: "gone"})
	if err != nil || c.Caption != "" {
		t.Fatalf("missing post: %+v %v", c, err)
	}
}

func TestAdapter_ListContainersMapsCounts(t *testing.T) {
	media := &fakeMediaRepo{ListByAccountFn: func(_ context.Context, acc string, limit, offset int) ([]*igdomain.Media, error) {
		if offset > 0 {
			return nil, nil
		}
		return []*igdomain.Media{{WorkspaceID: "ws-1", IGAccountID: acc, IGMediaID: "m-1", CommentsCount: 12}}, nil
	}}
	a := NewCommentAnalysisAdapter(&captureIngestor{}, &fakeCommentTexts{}, media, nil, nil, &fakeAnalysisTombstones{})
	page, err := a.ListContainers(context.Background(), "acc-1", 100, 0)
	if err != nil || len(page) != 1 || page[0].CommentsCount != 12 || page[0].Ref.ContainerID != "m-1" || page[0].WorkspaceID != "ws-1" {
		t.Fatalf("page = %+v %v", page, err)
	}
}

// The verifier answers ownership from the account row, never trusting the
// caller's workspace id.
func TestAccountVerifier(t *testing.T) {
	accounts := &fakeAccountRepo{FindByIDFn: func(_ context.Context, id string) (*igdomain.Account, error) {
		if id == "acc-1" {
			return &igdomain.Account{ID: "acc-1", WorkspaceID: "ws-1"}, nil
		}
		return nil, igdomain.ErrAccountNotFound
	}}
	v := NewCommentAnalysisAccountVerifier(accounts)
	if ok, err := v.AccountBelongsTo(context.Background(), "ws-1", "acc-1"); err != nil || !ok {
		t.Fatalf("owner: %v %v", ok, err)
	}
	if ok, _ := v.AccountBelongsTo(context.Background(), "ws-2", "acc-1"); ok {
		t.Fatal("another workspace must not own the account")
	}
	if ok, err := v.AccountBelongsTo(context.Background(), "ws-1", "nope"); ok || !errors.Is(err, nil) {
		t.Fatalf("unknown account: %v %v", ok, err)
	}
}

type recordingEnqueuer struct {
	enqueued []*igdomain.Comment
}

func (r *recordingEnqueuer) Enqueue(_ context.Context, c *igdomain.Comment) {
	r.enqueued = append(r.enqueued, c)
}
func (r *recordingEnqueuer) Forget(context.Context, string) {}

// T-29: the one line on the hot path. After the mirror is written, the
// comment reaches the engine; without the port wired, nothing else changes.
func TestHandleComment_EnqueuesAfterMirror(t *testing.T) {
	comments := &fakeCommentRepo{}
	enq := &recordingEnqueuer{}
	uc := NewHandleWebhookUseCase(HandleWebhookDeps{Comments: comments, CommentAnalysis: enq})
	account := &igdomain.Account{ID: "acc-1", WorkspaceID: "ws-1", IGUserID: "ig-me"}
	ev := &igdomain.Event{
		Kind: igdomain.EventComment, Timestamp: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC),
		Comment: &igdomain.CommentValue{ID: "c-1", Text: "oi", From: &igdomain.CommentFrom{ID: "igsid-9", Username: "ana"},
			Media: &igdomain.CommentMedia{ID: "m-1"}},
	}
	if err := uc.handleComment(context.Background(), account, ev); err != nil {
		t.Fatal(err)
	}
	if comments.Upserts != 1 {
		t.Fatalf("mirror upserts = %d", comments.Upserts)
	}
	if len(enq.enqueued) != 1 || enq.enqueued[0].IGCommentID != "c-1" || enq.enqueued[0].WorkspaceID != "ws-1" {
		t.Fatalf("enqueued = %+v", enq.enqueued)
	}

	// Unwired: the webhook behaves exactly as before.
	plain := NewHandleWebhookUseCase(HandleWebhookDeps{Comments: comments})
	if err := plain.handleComment(context.Background(), account, ev); err != nil {
		t.Fatal(err)
	}
}
