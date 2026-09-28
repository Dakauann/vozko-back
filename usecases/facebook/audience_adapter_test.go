package facebook

import (
	"context"
	"testing"
	"time"

	ca "vozko/domain/audience"
	fbdomain "vozko/domain/facebook"
)

type recordingIngestor struct{ inputs []ca.IngestInput }

func (r *recordingIngestor) Enqueue(_ context.Context, in ca.IngestInput) error {
	r.inputs = append(r.inputs, in)
	return nil
}

type recordingTombstones struct{ ids []string }

func (r *recordingTombstones) SoftDeleteBySourceComment(_ context.Context, source ca.Source, id string, _ time.Time) error {
	r.ids = append(r.ids, string(source)+":"+id)
	return nil
}

func newAudienceFixture() (*AudienceAdapter, *recordingIngestor, *recordingTombstones, *fakeComments, *fakePosts, *fakeCommentService) {
	ingestor, tombstones := &recordingIngestor{}, &recordingTombstones{}
	comments, posts, service := newFakeComments(), newFakePosts(), &fakeCommentService{remote: map[string]*fbdomain.RemoteComment{}}
	adapter := NewAudienceAdapter(AudienceDeps{
		Ingestor: ingestor, Pages: newFakePages(moderatingPage()), Posts: posts, Comments: comments,
		Service: service, Tombstones: tombstones,
	})
	return adapter, ingestor, tombstones, comments, posts, service
}

func TestCommentsAreEnqueuedUnderTheirPageAndPost(t *testing.T) {
	adapter, ingestor, _, _, _, _ := newAudienceFixture()
	c := theirComment("1_c", time.Hour)
	parent := "1_p"
	c.ParentFBCommentID, c.Message = &parent, "que caro"

	adapter.Enqueue(context.Background(), c)

	in := ingestor.inputs[0]
	if in.Container.Source != ca.SourceFacebook || in.Container.AccountID != "page-1" || in.Container.ContainerID != "fb-page-1_1" {
		t.Fatalf("container = %+v", in.Container)
	}
	if in.SubjectID != "1_c" || in.ParentSubjectID != "1_p" || in.AuthorExternalID != "psid-9" || in.AuthorHandle != "Ana" || in.Text != "que caro" || in.OccurredAt.IsZero() {
		t.Fatalf("input = %+v", in)
	}
}

func TestACommentWithoutAPostIsNotEnqueued(t *testing.T) {
	adapter, ingestor, _, _, _, _ := newAudienceFixture()
	adapter.Enqueue(context.Background(), &fbdomain.Comment{FBCommentID: "x", PageID: "page-1"})
	if len(ingestor.inputs) != 0 {
		t.Fatal("an unanchored comment was enqueued")
	}
}

func TestForgetTombstonesTheAnalysis(t *testing.T) {
	adapter, _, tombstones, _, _, _ := newAudienceFixture()
	adapter.Forget(context.Background(), "1_c")
	if len(tombstones.ids) != 1 || tombstones.ids[0] != "facebook:1_c" {
		t.Fatalf("ids = %v", tombstones.ids)
	}
}

func TestReadTextsOnlyReturnsThisPagesComments(t *testing.T) {
	adapter, _, _, comments, _, _ := newAudienceFixture()
	mine := theirComment("1_c", time.Hour)
	mine.Message = "oi"
	other := theirComment("2_c", time.Hour)
	other.PageID = "page-2"
	_ = comments.UpsertMany(context.Background(), []*fbdomain.Comment{mine, other})

	texts, err := adapter.ReadTexts(context.Background(), ca.ContainerRef{Source: ca.SourceFacebook, AccountID: "page-1"}, []string{"1_c", "2_c", "missing"})
	if err != nil || len(texts) != 1 || texts["1_c"] != "oi" {
		t.Fatalf("texts %v, %v", texts, err)
	}
}

func TestContainerContextComesFromTheMirroredPost(t *testing.T) {
	adapter, _, _, _, posts, _ := newAudienceFixture()
	created := time.Now().UTC()
	posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1", PageID: "page-1", Message: "Promo de sábado", PermalinkURL: "https://fb/1", CreatedTime: &created}

	got, err := adapter.ReadContainerContext(context.Background(), ca.ContainerRef{Source: ca.SourceFacebook, AccountID: "page-1", ContainerID: "fb-page-1_1"})
	if err != nil || got.Caption != "Promo de sábado" || got.Permalink != "https://fb/1" || got.PublishedAt == nil {
		t.Fatalf("context %+v, %v", got, err)
	}
}

func TestBackfillMirrorsAndReturnsEveryComment(t *testing.T) {
	adapter, _, _, comments, _, service := newAudienceFixture()
	at := time.Now().UTC()
	service.listed = []*fbdomain.RemoteComment{
		{FBCommentID: "1_a", FromID: "psid-1", Message: "a", CreatedTime: &at},
		{FBCommentID: "1_b", FromID: "fb-page-1", Message: "resposta", ParentID: "1_a", CreatedTime: &at},
	}

	items, next, err := adapter.FetchCommentsPage(context.Background(), ca.ContainerRef{Source: ca.SourceFacebook, AccountID: "page-1", ContainerID: "fb-page-1_1"}, "")
	if err != nil || next != "" || len(items) != 2 {
		t.Fatalf("items %v next %q err %v", items, next, err)
	}
	if !items[1].IsOurs || items[1].ParentSubjectID != "1_a" {
		t.Fatalf("page reply = %+v", items[1])
	}
	if len(comments.byID) != 2 {
		t.Fatal("backfilled comments not mirrored")
	}
}

func TestPageOwnershipAnswersForTheAudience(t *testing.T) {
	owner := NewPageOwnership(newFakePages(moderatingPage()))
	if ok, err := owner.AccountBelongsTo(context.Background(), "ws-1", "page-1"); !ok || err != nil {
		t.Fatalf("own page: %v %v", ok, err)
	}
	if ok, err := owner.AccountBelongsTo(context.Background(), "ws-2", "page-1"); ok || err != nil {
		t.Fatalf("other workspace: %v %v", ok, err)
	}
	if ok, err := owner.AccountBelongsTo(context.Background(), "ws-1", "missing"); ok || err != nil {
		t.Fatalf("missing page: %v %v", ok, err)
	}
}
