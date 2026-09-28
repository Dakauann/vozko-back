package facebook

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/privatereply"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
	"vozko/usecases/metachannel"
)

type fakeCommentService struct {
	listed   []*fbdomain.RemoteComment
	remote   map[string]*fbdomain.RemoteComment
	created  []string
	edited   []string
	hidden   []bool
	liked    []bool
	deleted  []string
	getCalls int
}

func (f *fakeCommentService) List(context.Context, string, string, fbdomain.CommentFilter, int, string) (*fbdomain.Paged[*fbdomain.RemoteComment], error) {
	return &fbdomain.Paged[*fbdomain.RemoteComment]{Items: f.listed}, nil
}
func (f *fakeCommentService) Get(_ context.Context, _, id string) (*fbdomain.RemoteComment, error) {
	f.getCalls++
	if c, ok := f.remote[id]; ok {
		return c, nil
	}
	return nil, fbdomain.ErrCommentNotFound
}
func (f *fakeCommentService) Create(_ context.Context, _, parent, message string) (string, error) {
	f.created = append(f.created, parent+":"+message)
	return parent + "_new", nil
}
func (f *fakeCommentService) Edit(_ context.Context, _, id, message string) error {
	f.edited = append(f.edited, id+":"+message)
	return nil
}
func (f *fakeCommentService) SetHidden(_ context.Context, _, _ string, hidden bool) error {
	f.hidden = append(f.hidden, hidden)
	return nil
}
func (f *fakeCommentService) Delete(_ context.Context, _, id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *fakeCommentService) SetLiked(_ context.Context, _, _ string, liked bool) error {
	f.liked = append(f.liked, liked)
	return nil
}

type fakeComments struct {
	byID     map[string]*fbdomain.Comment
	linked   map[string]string
	removed  []string
	likeDiff map[string]int
}

func newFakeComments(existing ...*fbdomain.Comment) *fakeComments {
	f := &fakeComments{byID: map[string]*fbdomain.Comment{}, linked: map[string]string{}, likeDiff: map[string]int{}}
	for _, c := range existing {
		f.byID[c.FBCommentID] = c
	}
	return f
}

func (f *fakeComments) UpsertMany(_ context.Context, cs []*fbdomain.Comment) error {
	for _, c := range cs {
		if existing, ok := f.byID[c.FBCommentID]; ok {
			existing.Message, existing.IsHidden, existing.LikeCount = c.Message, c.IsHidden, c.LikeCount
			continue
		}
		copied := *c
		f.byID[c.FBCommentID] = &copied
	}
	return nil
}
func (f *fakeComments) FindByFBCommentID(_ context.Context, id string) (*fbdomain.Comment, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, fbdomain.ErrCommentNotFound
}
func (f *fakeComments) SetHidden(_ context.Context, id string, hidden bool) error {
	f.byID[id].IsHidden = hidden
	return nil
}
func (f *fakeComments) SetLiked(_ context.Context, id string, liked bool) error {
	f.byID[id].LikedByPage = liked
	return nil
}
func (f *fakeComments) Edit(_ context.Context, id, message string, at time.Time) error {
	f.byID[id].Message, f.byID[id].EditedAt = message, &at
	return nil
}
func (f *fakeComments) MarkRemoved(_ context.Context, id string, at time.Time) error {
	f.removed = append(f.removed, id)
	if c, ok := f.byID[id]; ok {
		c.RemovedAt = &at
	}
	return nil
}
func (f *fakeComments) AddLikes(_ context.Context, id string, delta int) error {
	f.likeDiff[id] += delta
	return nil
}
func (f *fakeComments) ContactsFor(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if c, ok := f.byID[id]; ok && c.ContactID != nil {
			out[id] = *c.ContactID
		}
	}
	return out, nil
}
func (f *fakeComments) LinkContact(_ context.Context, id, contactID string) error {
	f.linked[id] = contactID
	return nil
}

type fakeReplyRecords struct {
	claimed map[string]bool
	records map[string]*privatereply.Record
}

func newFakeReplyRecords() *fakeReplyRecords {
	return &fakeReplyRecords{claimed: map[string]bool{}, records: map[string]*privatereply.Record{}}
}
func (f *fakeReplyRecords) Claim(_ context.Context, _ shared.EntryType, id, _ string) (bool, error) {
	if f.claimed[id] {
		return false, nil
	}
	f.claimed[id] = true
	return true, nil
}
func (f *fakeReplyRecords) MarkSent(_ context.Context, _ shared.EntryType, id, recipient, mid string) error {
	f.records[id] = &privatereply.Record{CommentID: id, Status: privatereply.StatusSent, RecipientRef: &recipient, MessageID: &mid}
	return nil
}
func (f *fakeReplyRecords) MarkFailed(_ context.Context, _ shared.EntryType, id string, code int, _ string) error {
	f.records[id] = &privatereply.Record{CommentID: id, Status: privatereply.StatusFailed, ErrorCode: code}
	return nil
}
func (f *fakeReplyRecords) Find(_ context.Context, _ shared.EntryType, id string) (*privatereply.Record, error) {
	return f.records[id], nil
}
func (f *fakeReplyRecords) FindMany(_ context.Context, _ shared.EntryType, ids []string) (map[string]*privatereply.Record, error) {
	out := map[string]*privatereply.Record{}
	for _, id := range ids {
		if r, ok := f.records[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}

type recordingForgetter struct{ forgotten []string }

func (r *recordingForgetter) Forget(_ context.Context, id string) {
	r.forgotten = append(r.forgotten, id)
}

type commentFixture struct {
	uc        *CommentUseCases
	pages     *fakePages
	comments  *fakeComments
	service   *fakeCommentService
	messaging *fakeMessaging
	contacts  *fakeContacts
	convs     *fakeConversations
	records   *fakeReplyRecords
	history   *recordingHistory
	forgotten *recordingForgetter
}

func moderatingPage() *fbdomain.Page {
	p := publishingPage()
	p.GrantedScopes = append(p.GrantedScopes, fbdomain.ScopeManageEngagement)
	return p
}

func theirComment(id string, age time.Duration) *fbdomain.Comment {
	at := time.Now().UTC().Add(-age)
	from := "psid-9"
	return &fbdomain.Comment{PageID: "page-1", WorkspaceID: "ws-1", FBCommentID: id, FBPostID: "fb-page-1_1", FromID: &from, FromName: "Ana", CreatedTime: &at}
}

func newCommentFixture(existing ...*fbdomain.Comment) *commentFixture {
	f := &commentFixture{
		pages: newFakePages(moderatingPage()), comments: newFakeComments(existing...), service: &fakeCommentService{remote: map[string]*fbdomain.RemoteComment{}},
		messaging: &fakeMessaging{}, contacts: newFakeContacts(), convs: newFakeConversations(),
		records: newFakeReplyRecords(), history: &recordingHistory{}, forgotten: &recordingForgetter{},
	}
	f.uc = NewCommentUseCases(CommentDeps{
		Pages: f.pages, Comments: f.comments, Service: f.service, Messaging: f.messaging,
		Contacts: f.contacts, Conversations: f.convs,
		Transcript:     &metachannel.Transcript{EntryType: shared.EntryTypeFacebook, Channel: conversation.MessageChannelFacebook, Prefix: MetadataPrefix, History: f.history},
		PrivateReplies: cauc.NewPrivateReplySender(f.records), ReplyRecords: f.records, Audience: f.forgotten,
	})
	return f
}

func TestListShowsPrivateReplyStateAndMirrors(t *testing.T) {
	f := newCommentFixture()
	created := time.Now().UTC().Add(-time.Hour)
	f.service.listed = []*fbdomain.RemoteComment{{FBCommentID: "1_c", FromID: "psid-9", Message: "preço?", CreatedTime: &created, CanReplyPrivately: true}}
	mid := "m_1"
	f.records.records["1_c"] = &privatereply.Record{CommentID: "1_c", Status: privatereply.StatusSent, MessageID: &mid}

	page, err := f.uc.List(context.Background(), "ws-1", "page-1", "fb-page-1_1", fbdomain.CommentsStream, 0, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	view := page.Items[0]
	if view.PrivateReply == nil || view.PrivateReply.Status != privatereply.StatusSent || view.Deadline == nil {
		t.Fatalf("view = %+v", view)
	}
	if f.comments.byID["1_c"] == nil {
		t.Fatal("listed comment not mirrored")
	}
}

func TestCommentsOfAnotherPagesPostAreNotListed(t *testing.T) {
	f := newCommentFixture()
	if _, err := f.uc.List(context.Background(), "ws-1", "page-1", "other_1", fbdomain.CommentsStream, 0, ""); !errors.Is(err, fbdomain.ErrPostNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestModerationNeedsTheCommentOnThisPage(t *testing.T) {
	foreign := theirComment("9_c", time.Hour)
	foreign.PageID = "page-2"
	f := newCommentFixture(foreign)
	ctx := context.Background()

	if err := f.uc.SetHidden(ctx, "ws-1", "page-1", "9_c", true); !errors.Is(err, fbdomain.ErrCommentNotFound) {
		t.Errorf("hide: %v", err)
	}
	if err := f.uc.Delete(ctx, "ws-1", "page-1", "unknown"); !errors.Is(err, fbdomain.ErrCommentNotFound) {
		t.Errorf("delete: %v", err)
	}
	if len(f.service.hidden)+len(f.service.deleted) != 0 {
		t.Fatal("a foreign comment reached Graph")
	}
}

func TestHideDeleteAndLikeAreMirrored(t *testing.T) {
	f := newCommentFixture(theirComment("1_c", time.Hour))
	ctx := context.Background()

	if err := f.uc.SetHidden(ctx, "ws-1", "page-1", "1_c", true); err != nil || !f.comments.byID["1_c"].IsHidden {
		t.Fatalf("hide: %v", err)
	}
	if err := f.uc.SetLiked(ctx, "ws-1", "page-1", "1_c", true); err != nil || !f.comments.byID["1_c"].LikedByPage {
		t.Fatalf("like: %v", err)
	}
	if err := f.uc.Delete(ctx, "ws-1", "page-1", "1_c"); err != nil || len(f.comments.removed) != 1 {
		t.Fatalf("delete: %v", err)
	}
	if len(f.forgotten.forgotten) != 1 || f.forgotten.forgotten[0] != "1_c" {
		t.Fatalf("audience not told to forget: %v", f.forgotten.forgotten)
	}
}

func TestOnlyOurOwnCommentCanBeEdited(t *testing.T) {
	ours := theirComment("1_ours", time.Hour)
	ours.IsOurs = true
	f := newCommentFixture(theirComment("1_c", time.Hour), ours)
	ctx := context.Background()

	if err := f.uc.Edit(ctx, "ws-1", "page-1", "1_c", "x"); !errors.Is(err, fbdomain.ErrCommentNotOurs) {
		t.Fatalf("edit theirs: %v", err)
	}
	if err := f.uc.Edit(ctx, "ws-1", "page-1", "1_ours", "corrigido"); err != nil || f.comments.byID["1_ours"].Message != "corrigido" {
		t.Fatalf("edit ours: %v", err)
	}
}

func TestReplyAndCommentAsPageAreMirroredAsOurs(t *testing.T) {
	f := newCommentFixture(theirComment("1_c", time.Hour))
	ctx := context.Background()

	id, err := f.uc.Reply(ctx, "ws-1", "page-1", "1_c", "Oi Ana")
	if err != nil || f.comments.byID[id] == nil || !f.comments.byID[id].IsOurs || *f.comments.byID[id].ParentFBCommentID != "1_c" {
		t.Fatalf("reply %q: %v", id, err)
	}
	top, err := f.uc.CommentAsPage(ctx, "ws-1", "page-1", "fb-page-1_1", "Novidades")
	if err != nil || f.comments.byID[top] == nil || f.comments.byID[top].FBPostID != "fb-page-1_1" {
		t.Fatalf("top %q: %v", top, err)
	}
	if _, err := f.uc.Reply(ctx, "ws-1", "page-1", "1_c", "  "); !errors.Is(err, fbdomain.ErrCommentEmpty) {
		t.Fatalf("empty reply: %v", err)
	}
}

func TestPrivateReplyLandsInTheCommentersConversation(t *testing.T) {
	f := newCommentFixture(theirComment("1_c", time.Hour))
	f.messaging.sent = nil

	out, err := f.uc.SendPrivateReply(context.Background(), "ws-1", "page-1", "1_c", conversation.SentByPerson("user-1"), "Te chamei no direct")
	if err != nil {
		t.Fatal(err)
	}
	sent := f.messaging.sent[0]
	if sent.Recipient.CommentID != "1_c" || sent.Recipient.PSID != "" || sent.Text != "Te chamei no direct" {
		t.Fatalf("sent = %+v", sent)
	}
	if out.ConversationID == "" || out.MessageID == "" {
		t.Fatalf("out = %+v", out)
	}
	if f.comments.linked["1_c"] == "" {
		t.Fatal("comment not linked to the contact")
	}
	if len(f.history.records) != 1 || f.history.records[0].MessageType != conversation.MessageTypeOperator || len(f.convs.outbound) != 1 {
		t.Fatalf("history %+v outbound %v", f.history.records, f.convs.outbound)
	}
}

func TestPrivateReplyUsesGraphWhenTheCommentTimeIsMissing(t *testing.T) {
	undated := theirComment("1_c", 0)
	undated.CreatedTime = nil
	f := newCommentFixture(undated)
	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	f.service.remote["1_c"] = &fbdomain.RemoteComment{FBCommentID: "1_c", CreatedTime: &old}

	_, err := f.uc.SendPrivateReply(context.Background(), "ws-1", "page-1", "1_c", conversation.SentByPerson("u"), "oi")
	if !errors.Is(err, privatereply.ErrExpired) || f.service.getCalls != 1 || len(f.messaging.sent) != 0 {
		t.Fatalf("err %v, get %d, sent %d", err, f.service.getCalls, len(f.messaging.sent))
	}
}

func TestPrivateReplyNeedsMessaging(t *testing.T) {
	f := newCommentFixture(theirComment("1_c", time.Hour))
	f.pages.byID["page-1"].Tasks = []fbdomain.Task{fbdomain.TaskModerate}

	if _, err := f.uc.SendPrivateReply(context.Background(), "ws-1", "page-1", "1_c", conversation.SentByPerson("u"), "oi"); !errors.Is(err, fbdomain.ErrCapabilityDenied) {
		t.Fatalf("got %v", err)
	}
}
