package facebook

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	fbdomain "vozko/domain/facebook"
	mm "vozko/domain/metamessaging"
)

func feedEntry(changes ...*mm.Change) *mm.EntryEnvelope {
	return &mm.EntryEnvelope{Object: "page", Entry: &mm.Entry{ID: "fb-page-1", Time: 1, Changes: changes}}
}

func feedChange(field, value string) *mm.Change {
	return &mm.Change{Field: field, Value: json.RawMessage(value)}
}

type recordingRules struct{ evaluated []string }

func (r *recordingRules) Evaluate(_ context.Context, _ *fbdomain.Page, c *fbdomain.Comment) {
	r.evaluated = append(r.evaluated, c.FBCommentID)
}

type recordingAudience struct {
	enqueued  []string
	forgotten []string
}

func (r *recordingAudience) Enqueue(_ context.Context, c *fbdomain.Comment) {
	r.enqueued = append(r.enqueued, c.FBCommentID+":"+c.Message)
}
func (r *recordingAudience) Forget(_ context.Context, id string) {
	r.forgotten = append(r.forgotten, id)
}

type recordingVideos struct{ resolved []string }

func (r *recordingVideos) ResolveVideo(_ context.Context, videoID, state string) error {
	r.resolved = append(r.resolved, videoID+":"+state)
	return nil
}

type feedFixture struct {
	videos   *recordingVideos
	uc       *HandleFeedUseCase
	posts    *fakePosts
	pages    *fakePages
	comments *fakeComments
	contacts *fakeContacts
	service  *fakeCommentService
	rules    *recordingRules
	audience *recordingAudience
}

func newFeedFixture() *feedFixture {
	f := &feedFixture{
		posts: newFakePosts(), pages: newFakePages(publishingPage()), comments: newFakeComments(), contacts: newFakeContacts(),
		service: &fakeCommentService{remote: map[string]*fbdomain.RemoteComment{}}, rules: &recordingRules{}, audience: &recordingAudience{},
		videos: &recordingVideos{},
	}
	f.uc = NewHandleFeedUseCase(HandleFeedDeps{
		Pages: f.pages, Posts: f.posts, Comments: f.comments, Contacts: f.contacts,
		Service: f.service, Rules: f.rules, Audience: f.audience, Videos: f.videos,
	})
	return f
}

func TestANewCommentIsMirroredLinkedRuledAndAnalysed(t *testing.T) {
	f := newFeedFixture()
	f.posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1", PageID: "page-1"}

	err := f.uc.Execute(context.Background(), feedEntry(feedChange("feed",
		`{"item":"comment","verb":"add","comment_id":"1_c","post_id":"fb-page-1_1","parent_id":"fb-page-1_1","from":{"id":"PSID","name":"Ana"},"created_time":1790000000,"message":"preço?"}`)))
	if err != nil {
		t.Fatal(err)
	}
	c := f.comments.byID["1_c"]
	if c == nil || c.ContactID == nil || *c.ContactID != "contact-PSID" || c.IsOurs {
		t.Fatalf("comment = %+v", c)
	}
	if len(f.rules.evaluated) != 1 || len(f.audience.enqueued) != 1 || f.posts.byFBID["fb-page-1_1"].CommentsCount != 1 {
		t.Fatalf("rules %v audience %v count %d", f.rules.evaluated, f.audience.enqueued, f.posts.byFBID["fb-page-1_1"].CommentsCount)
	}
}

func TestThePagesOwnCommentTriggersNothing(t *testing.T) {
	f := newFeedFixture()
	_ = f.uc.Execute(context.Background(), feedEntry(feedChange("feed",
		`{"item":"comment","verb":"add","comment_id":"1_c","post_id":"fb-page-1_1","from":{"id":"fb-page-1","name":"Loja"},"message":"obrigado"}`)))
	c := f.comments.byID["1_c"]
	if c == nil || !c.IsOurs || c.ContactID != nil || len(f.rules.evaluated)+len(f.audience.enqueued) != 0 {
		t.Fatalf("comment %+v rules %v audience %v", c, f.rules.evaluated, f.audience.enqueued)
	}
}

func TestAMissingAuthorIsReadFromGraphOnce(t *testing.T) {
	f := newFeedFixture()
	f.service.remote["1_c"] = &fbdomain.RemoteComment{FBCommentID: "1_c", FromID: "PSID", FromName: "Ana"}
	_ = f.uc.Execute(context.Background(), feedEntry(feedChange("feed",
		`{"item":"comment","verb":"add","comment_id":"1_c","post_id":"fb-page-1_1","message":"oi"}`)))
	if c := f.comments.byID["1_c"]; c == nil || c.FromID == nil || *c.FromID != "PSID" || f.service.getCalls != 1 {
		t.Fatalf("comment %+v gets %d", c, f.service.getCalls)
	}
}

func TestAnAuthorGraphCannotNameStaysUnknown(t *testing.T) {
	f := newFeedFixture()
	_ = f.uc.Execute(context.Background(), feedEntry(feedChange("feed",
		`{"item":"comment","verb":"add","comment_id":"1_c","post_id":"fb-page-1_1","message":"oi"}`)))
	if c := f.comments.byID["1_c"]; c == nil || c.FromID != nil || c.ContactID != nil {
		t.Fatalf("comment %+v", c)
	}
}

func TestCommentEditsRemovalsAndHides(t *testing.T) {
	f := newFeedFixture()
	f.comments.byID["1_c"] = theirComment("1_c", time.Hour)
	f.posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1", CommentsCount: 3}
	ctx := context.Background()

	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"comment","verb":"edited","comment_id":"1_c","post_id":"fb-page-1_1","message":"novo"}`)))
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"comment","verb":"hide","comment_id":"1_c","post_id":"fb-page-1_1"}`)))
	if c := f.comments.byID["1_c"]; c.Message != "novo" || c.EditedAt == nil || !c.IsHidden {
		t.Fatalf("comment %+v", c)
	}
	if len(f.audience.enqueued) != 1 || f.audience.enqueued[0] != "1_c:novo" {
		t.Fatalf("edit not re-analysed: %v", f.audience.enqueued)
	}
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"comment","verb":"remove","comment_id":"1_c","post_id":"fb-page-1_1"}`)))
	if f.comments.byID["1_c"].RemovedAt == nil || len(f.audience.forgotten) != 1 || f.posts.byFBID["fb-page-1_1"].CommentsCount != 2 {
		t.Fatalf("remove: comment %+v forgotten %v count %d", f.comments.byID["1_c"], f.audience.forgotten, f.posts.byFBID["fb-page-1_1"].CommentsCount)
	}
}

func TestReactionsMoveTheRightCounter(t *testing.T) {
	f := newFeedFixture()
	f.posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1"}
	ctx := context.Background()

	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"reaction","verb":"add","post_id":"fb-page-1_1","reaction_type":"love"}`)))
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"reaction","verb":"remove","post_id":"fb-page-1_1","comment_id":"1_c","reaction_type":"like"}`)))
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"reaction","verb":"edit","post_id":"fb-page-1_1","reaction_type":"wow"}`)))

	if f.posts.byFBID["fb-page-1_1"].ReactionsCount != 1 || f.comments.likeDiff["1_c"] != -1 {
		t.Fatalf("post %d comment %d", f.posts.byFBID["fb-page-1_1"].ReactionsCount, f.comments.likeDiff["1_c"])
	}
}

func TestOwnAndVisitorPostsAreTracked(t *testing.T) {
	f := newFeedFixture()
	err := f.uc.Execute(context.Background(), feedEntry(
		feedChange("feed", `{"item":"photo","verb":"add","post_id":"fb-page-1_1","from":{"id":"fb-page-1"},"published":1,"message":"Foto"}`),
		feedChange("feed", `{"item":"post","verb":"add","post_id":"fb-page-1_2","from":{"id":"USER"},"message":"Oi loja"}`),
	))
	if err != nil {
		t.Fatal(err)
	}
	own, visitor := f.posts.byFBID["fb-page-1_1"], f.posts.byFBID["fb-page-1_2"]
	if own == nil || own.Kind != fbdomain.PostPhoto || !own.IsPublished || own.Message != "Foto" || own.PageID != "page-1" {
		t.Fatalf("own = %+v", own)
	}
	if visitor == nil || visitor.Kind != fbdomain.PostVisitor {
		t.Fatalf("visitor = %+v", visitor)
	}
}

func TestAScheduledPostArrivesUnpublished(t *testing.T) {
	f := newFeedFixture()
	_ = f.uc.Execute(context.Background(), feedEntry(feedChange("feed", `{"item":"status","verb":"add","post_id":"fb-page-1_3","from":{"id":"fb-page-1"},"published":0}`)))
	if p := f.posts.byFBID["fb-page-1_3"]; p == nil || p.IsPublished {
		t.Fatalf("post = %+v", p)
	}
}

func TestPostEditsHidesAndRemovalsAreMirrored(t *testing.T) {
	f := newFeedFixture()
	f.posts.byFBID["fb-page-1_1"] = &fbdomain.Post{FBPostID: "fb-page-1_1", Message: "antes"}
	ctx := context.Background()

	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"status","verb":"edited","post_id":"fb-page-1_1","message":"depois"}`)))
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"status","verb":"hide","post_id":"fb-page-1_1"}`)))
	if p := f.posts.byFBID["fb-page-1_1"]; p.Message != "depois" || !p.IsHidden {
		t.Fatalf("post = %+v", p)
	}
	_ = f.uc.Execute(ctx, feedEntry(feedChange("feed", `{"item":"status","verb":"remove","post_id":"fb-page-1_1"}`)))
	if _, ok := f.posts.byFBID["fb-page-1_1"]; ok {
		t.Fatal("removed post still mirrored")
	}
}

func TestFeedForAnUnknownPageIsRejected(t *testing.T) {
	f := newFeedFixture()
	env := feedEntry(feedChange("feed", `{"item":"status","verb":"add","post_id":"x_1"}`))
	env.Entry.ID = "someone-else"
	if err := f.uc.Execute(context.Background(), env); !errors.Is(err, ErrUnknownPage) {
		t.Fatalf("got %v", err)
	}
}

func TestMalformedChangeIsReportedButTheRestIsHandled(t *testing.T) {
	f := newFeedFixture()
	err := f.uc.Execute(context.Background(), feedEntry(
		feedChange("feed", `{"item":`),
		feedChange("feed", `{"item":"status","verb":"add","post_id":"fb-page-1_5","from":{"id":"fb-page-1"}}`),
	))
	if !errors.Is(err, mm.ErrInvalidWebhookPayload) {
		t.Fatalf("got %v", err)
	}
	if f.posts.byFBID["fb-page-1_5"] == nil {
		t.Fatal("the valid change was dropped")
	}
}

func TestPostEventsForAnotherPagesPostAreIgnored(t *testing.T) {
	f := newFeedFixture()
	_ = f.uc.Execute(context.Background(), feedEntry(feedChange("feed", `{"item":"status","verb":"add","post_id":"other_1","from":{"id":"other"}}`)))
	if len(f.posts.byFBID) != 0 {
		t.Fatalf("mirrored %v", f.posts.byFBID)
	}
}

func TestVideoStatusResolvesThePublishJob(t *testing.T) {
	f := newFeedFixture()
	if err := f.uc.Execute(context.Background(), feedEntry(feedChange("videos", `{"id":"V1","status":{"video_status":"ready"}}`))); err != nil {
		t.Fatal(err)
	}
	if len(f.videos.resolved) != 1 || f.videos.resolved[0] != "V1:ready" {
		t.Fatalf("resolved %v", f.videos.resolved)
	}
}
