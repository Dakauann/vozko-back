package tiktok

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ca "vozko/domain/audience"
	rules "vozko/domain/commentautomation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	commentautomation "vozko/usecases/commentautomation"
)

type fakeVideos struct {
	mu   sync.Mutex
	byID map[string]*ttdomain.Video
}

func newFakeVideos() *fakeVideos { return &fakeVideos{byID: map[string]*ttdomain.Video{}} }

func (f *fakeVideos) UpsertMany(ctx context.Context, videos []*ttdomain.Video) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range videos {
		cp := *v
		f.byID[v.ItemID] = &cp
	}
	return nil
}

func (f *fakeVideos) FindByItemID(ctx context.Context, accountID, itemID string) (*ttdomain.Video, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.byID[itemID]; ok {
		return v, nil
	}
	return nil, ttdomain.ErrVideoNotFound
}

func (f *fakeVideos) FindByItemIDs(ctx context.Context, accountID string, ids []string) ([]*ttdomain.Video, error) {
	var out []*ttdomain.Video
	for _, id := range ids {
		if v, err := f.FindByItemID(ctx, accountID, id); err == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeVideos) ListByAccount(ctx context.Context, in ttdomain.ListVideosInput) (*shared.PaginatedResult[*ttdomain.Video], error) {
	return &shared.PaginatedResult[*ttdomain.Video]{}, nil
}

func (f *fakeVideos) ListSlice(ctx context.Context, accountID string, limit, offset int) ([]*ttdomain.Video, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ttdomain.Video
	for _, v := range f.byID {
		out = append(out, v)
	}
	return out, nil
}

type memoryRules struct {
	rules []*rules.Rule
}

func (m *memoryRules) Create(ctx context.Context, r *rules.Rule) error { return nil }
func (m *memoryRules) Update(ctx context.Context, r *rules.Rule) error { return nil }
func (m *memoryRules) Delete(ctx context.Context, workspaceID, id string) error {
	return nil
}
func (m *memoryRules) FindByID(ctx context.Context, workspaceID, id string) (*rules.Rule, error) {
	return nil, rules.ErrRuleNotFound
}
func (m *memoryRules) ListByAccount(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) ([]*rules.Rule, error) {
	return m.rules, nil
}
func (m *memoryRules) ListCandidates(ctx context.Context, source shared.EntryType, accountID, containerID string) ([]*rules.Rule, error) {
	return m.rules, nil
}

type memoryClaims struct {
	mu   sync.Mutex
	seen map[string]bool
	err  error
}

func (m *memoryClaims) Claim(ctx context.Context, source shared.EntryType, commentID, ruleID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	key := string(source) + ":" + commentID
	if m.seen[key] {
		return false, nil
	}
	m.seen[key] = true
	return true, nil
}

func (m *memoryClaims) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}

type fakeIngestor struct {
	mu     sync.Mutex
	inputs []ca.IngestInput
}

func (f *fakeIngestor) Enqueue(ctx context.Context, in ca.IngestInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	return nil
}

type fakeTombstones struct {
	ids []string
}

func (f *fakeTombstones) SoftDeleteBySourceComment(ctx context.Context, source ca.Source, id string, now time.Time) error {
	f.ids = append(f.ids, string(source)+":"+id)
	return nil
}

type commentHarness struct {
	account  *ttdomain.Account
	accounts *fakeAccounts
	contacts *fakeContacts
	comments *fakeComments
	videos   *fakeVideos
	svc      *fakeServices
	uc       *Comments
	actions  *RuleActions
	worker   *CommentWorker
	rules    *memoryRules
	claims   *memoryClaims
	ingestor *fakeIngestor
	audience *AudienceAdapter
	objects  *fakeObjects
	metrics  *fakeMetrics
}

func newCommentHarness(t *testing.T) commentHarness {
	t.Helper()
	accounts := newFakeAccounts()
	account := accounts.put(connectedAccount())
	svc := newFakeServices()
	metrics := &fakeMetrics{}
	tokens := NewTokenSource(accounts, svc, fakeClassifier{}, &fakeAlerts{}, metrics)
	tokens.now = fixedClock
	objects := &fakeObjects{}
	h := commentHarness{account: account, accounts: accounts, contacts: newFakeContacts(), comments: newFakeComments(), videos: newFakeVideos(), svc: svc, metrics: metrics, objects: objects}
	h.uc = NewComments(CommentsDeps{
		Accounts: accounts, Videos: h.videos, Comments: h.comments, Tokens: tokens, Services: svc.services(fakeClassifier{}),
		Images: NewImageStore(&fakeFetcher{data: map[string][]byte{"https://p16/t.jpg": []byte("\xff\xd8\xff\xe0")}}, objects), Metrics: metrics,
	})
	h.uc.now = fixedClock
	h.actions = NewRuleActions(accounts, h.uc)
	h.rules = &memoryRules{}
	h.claims = &memoryClaims{}
	h.ingestor = &fakeIngestor{}
	h.audience = NewAudienceAdapter(AudienceAdapterDeps{Ingestor: h.ingestor, Tombstones: &fakeTombstones{}, Accounts: accounts, Videos: h.videos, Comments: h.comments, Tokens: tokens, Service: svc, Metrics: metrics})
	h.worker = NewCommentWorker(CommentWorkerDeps{
		Accounts: accounts, Contacts: h.contacts, Comments: h.comments, Tokens: tokens, Service: svc, Errors: fakeClassifier{},
		Rules: commentautomation.NewEvaluator(h.rules, h.claims), Actions: h.actions, Audience: h.audience, Metrics: metrics,
	})
	h.worker.now = fixedClock
	return h
}

func (h commentHarness) mirror(commentID string, ours bool) {
	o := ours
	h.comments.byID[commentID] = &ttdomain.Comment{ID: "row-" + commentID, WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, VideoID: "7203946942097902849", CommentID: commentID, Visibility: ttdomain.VisibilityPublic, IsOurs: &o, Lookup: ttdomain.LookupResolved}
}

func TestReplyAnswersInTheThreadAndRecordsTheReplyAsOurs(t *testing.T) {
	h := newCommentHarness(t)
	h.mirror("100", false)
	reply, err := h.uc.Reply(context.Background(), h.account.WorkspaceID, h.account.ID, "100", "Obrigado!")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.svc.replies) != 1 || h.svc.replies[0].commentID != "100" || h.svc.replies[0].videoID != "7203946942097902849" {
		t.Fatalf("replies = %+v", h.svc.replies)
	}
	if len(h.comments.ours) != 1 || h.comments.ours[0].CommentID != reply.CommentID || h.comments.ours[0].ParentCommentID != "100" {
		t.Fatalf("ours = %+v", h.comments.ours)
	}
}

func TestReplyingToAReplyAnswersItsParentThread(t *testing.T) {
	h := newCommentHarness(t)
	h.mirror("101", false)
	h.comments.byID["101"].ParentCommentID = "100"
	if _, err := h.uc.Reply(context.Background(), h.account.WorkspaceID, h.account.ID, "101", "Oi"); err != nil {
		t.Fatal(err)
	}
	if h.svc.replies[0].commentID != "100" {
		t.Fatalf("replied to %s, want the parent", h.svc.replies[0].commentID)
	}
}

func TestModerationOnlyTouchesCommentsMirroredForTheAccount(t *testing.T) {
	h := newCommentHarness(t)
	if _, err := h.uc.SetHidden(context.Background(), h.account.WorkspaceID, h.account.ID, "999", true); !errors.Is(err, ttdomain.ErrCommentNotOwned) {
		t.Fatalf("unknown comment: %v", err)
	}
	if _, err := h.uc.Reply(context.Background(), "other-ws", h.account.ID, "999", "x"); !errors.Is(err, ttdomain.ErrAccountNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	if len(h.svc.hidden)+len(h.svc.replies) != 0 {
		t.Fatal("tiktok was called for a comment the account does not own")
	}
}

func TestReplyRefusesEmptyAndTooLongText(t *testing.T) {
	h := newCommentHarness(t)
	h.mirror("100", false)
	if _, err := h.uc.Reply(context.Background(), h.account.WorkspaceID, h.account.ID, "100", " "); !errors.Is(err, ttdomain.ErrCommentTextEmpty) {
		t.Errorf("empty: %v", err)
	}
}

func TestHideAndUnhideUpdateTheMirror(t *testing.T) {
	h := newCommentHarness(t)
	h.mirror("100", false)
	if c, err := h.uc.SetHidden(context.Background(), h.account.WorkspaceID, h.account.ID, "100", true); err != nil || c.Visibility != ttdomain.VisibilityHidden {
		t.Fatalf("hide: %+v %v", c, err)
	}
	h.svc.lookup["100"] = ttdomain.RemoteComment{CommentID: "100", Visibility: ttdomain.VisibilityHidden}
	c, err := h.uc.SetHidden(context.Background(), h.account.WorkspaceID, h.account.ID, "100", false)
	if err != nil || c.Visibility != ttdomain.VisibilityHidden {
		t.Fatalf("an unhide TikTok silently refused must stay hidden: %+v %v", c, err)
	}
}

func TestListVideosRehostsThumbnailsAndMirrors(t *testing.T) {
	h := newCommentHarness(t)
	h.svc.videos = ttdomain.VideoPage{Videos: []ttdomain.RemoteVideo{{ItemID: "7044071604573310209", Caption: "Promo", ThumbnailURL: "https://p16/t.jpg"}}, HasMore: true, Cursor: 5}
	listing, err := h.uc.ListVideos(context.Background(), h.account.WorkspaceID, h.account.ID, 0)
	if err != nil || len(listing.Videos) != 1 || !listing.HasMore || listing.Cursor != 5 {
		t.Fatalf("listing %+v err %v", listing, err)
	}
	if listing.Videos[0].ThumbnailKey != VideoThumbnailKey(h.account.ID, "7044071604573310209") {
		t.Errorf("thumbnail = %q", listing.Videos[0].ThumbnailKey)
	}
	if _, err := h.videos.FindByItemID(context.Background(), h.account.ID, "7044071604573310209"); err != nil {
		t.Error("the video was not mirrored")
	}
}

func TestRuleActionsSupportPublicRepliesHidesAndLikesOnly(t *testing.T) {
	h := newCommentHarness(t)
	h.mirror("100", false)
	rule := &rules.Rule{ID: "r1", WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, Source: shared.EntryTypeTikTok}
	ctx := context.Background()
	if err := h.actions.ReplyPublicly(ctx, rule, "100", "Oi"); err != nil {
		t.Errorf("reply: %v", err)
	}
	if err := h.actions.Hide(ctx, rule, "100"); err != nil {
		t.Errorf("hide: %v", err)
	}
	if err := h.actions.Like(ctx, rule, "100"); err != nil {
		t.Errorf("like: %v", err)
	}
	if err := h.actions.ReplyPrivately(ctx, rule, "100", "x"); !errors.Is(err, rules.ErrActionUnsupported) {
		t.Errorf("private reply: %v", err)
	}
	if err := h.actions.Delete(ctx, rule, "100"); !errors.Is(err, rules.ErrActionUnsupported) {
		t.Errorf("delete: %v", err)
	}
	other := &rules.Rule{ID: "r2", WorkspaceID: "someone-else", AccountID: h.account.ID, Source: shared.EntryTypeTikTok}
	if err := h.actions.Hide(ctx, other, "100"); !errors.Is(err, ErrAccountUnavailable) {
		t.Errorf("a rule of another workspace must not act: %v", err)
	}
}

func queued(h commentHarness, commentID, text string) *ttdomain.Comment {
	c := &ttdomain.Comment{ID: "row-" + commentID, WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, VideoID: "7203946942097902849", CommentID: commentID, Text: text, Visibility: ttdomain.VisibilityPublic, Lookup: ttdomain.LookupPending, RulesDue: true}
	h.comments.byID[commentID] = c
	return c
}

func TestTheWorkerResolvesAuthorsInBatchesAndRunsRulesExactlyOnce(t *testing.T) {
	h := newCommentHarness(t)
	h.rules.rules = []*rules.Rule{{ID: "r1", WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, Source: shared.EntryTypeTikTok, Name: "Preço", Enabled: true, Match: rules.MatchContains, Keywords: []string{"preço"}, Actions: []rules.Action{rules.ActionPublicReply}, PublicReplyText: "Oi {{username}}, te chamei no direct!"}}
	first := queued(h, "200", "Qual o preço?")
	second := queued(h, "201", "Lindo!")
	h.svc.lookup["200"] = ttdomain.RemoteComment{CommentID: "200", VideoID: first.VideoID, UniqueIdentifier: "+ana==", Username: "ana", DisplayName: "Ana", Text: "Qual o preço?", Visibility: ttdomain.VisibilityPublic}
	h.svc.lookup["201"] = ttdomain.RemoteComment{CommentID: "201", VideoID: second.VideoID, UniqueIdentifier: "+bia==", Username: "bia", Text: "Lindo!", Visibility: ttdomain.VisibilityPublic}
	h.comments.leased = []*ttdomain.Comment{first, second}
	if err := h.worker.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.lookupCalls) != 1 || len(h.svc.lookupCalls[0]) != 2 {
		t.Fatalf("lookups = %v, want one batched call", h.svc.lookupCalls)
	}
	if len(h.svc.replies) != 1 || h.svc.replies[0].text != "Oi ana, te chamei no direct!" {
		t.Fatalf("replies = %+v", h.svc.replies)
	}
	if h.comments.result["row-200"] != "resolved+rules" || h.comments.result["row-201"] != "resolved+rules" {
		t.Fatalf("results = %v", h.comments.result)
	}
	if len(h.ingestor.inputs) != 2 || h.ingestor.inputs[0].Container.Source != ca.SourceTikTok {
		t.Fatalf("audience = %+v", h.ingestor.inputs)
	}
	if contact, err := h.contacts.FindByUniqueIdentifier(context.Background(), h.account.ID, "+ana=="); err != nil || contact.DisplayName != "Ana" {
		t.Errorf("commenter contact %+v err %v", contact, err)
	}

	again := queued(h, "200", "Qual o preço?")
	again.Lookup = ttdomain.LookupResolved
	no := false
	again.IsOurs = &no
	h.comments.leased = []*ttdomain.Comment{again}
	if err := h.worker.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.replies) != 1 {
		t.Fatalf("a redelivered comment was answered twice: %+v", h.svc.replies)
	}
}

func TestTheWorkerNeverRunsRulesOnTheAccountsOwnComments(t *testing.T) {
	h := newCommentHarness(t)
	h.rules.rules = []*rules.Rule{{ID: "r1", WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, Source: shared.EntryTypeTikTok, Name: "Todos", Enabled: true, Match: rules.MatchAny, Actions: []rules.Action{rules.ActionPublicReply}, PublicReplyText: "Oi"}}
	c := queued(h, "300", "Respondendo")
	h.svc.lookup["300"] = ttdomain.RemoteComment{CommentID: "300", VideoID: c.VideoID, UniqueIdentifier: "+loja==", Owner: true, Text: "Respondendo"}
	h.comments.leased = []*ttdomain.Comment{c}
	if err := h.worker.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.svc.replies) != 0 {
		t.Fatal("the account answered its own comment")
	}
	if got := h.accounts.get(h.account.ID); got.UniqueIdentifier != "+loja==" {
		t.Errorf("the account's own identifier was not learned: %q", got.UniqueIdentifier)
	}
}

func TestALookupFailureDefersTheCommentAndGivesUpAfterTheLimit(t *testing.T) {
	h := newCommentHarness(t)
	h.svc.lookupErr = codedError{rate: true, retry: true}
	c := queued(h, "400", "oi")
	h.comments.leased = []*ttdomain.Comment{c}
	if err := h.worker.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.comments.result["row-400"] != "deferred" {
		t.Fatalf("result = %v", h.comments.result)
	}
	c.LookupAttempts = ttdomain.MaxLookupAttempts - 1
	h.comments.leased = []*ttdomain.Comment{c}
	_ = h.worker.Execute(context.Background())
	if h.comments.result["row-400"] != "failed" {
		t.Fatalf("result = %v", h.comments.result)
	}
	if len(h.svc.replies) != 0 {
		t.Fatal("no rule may run without the author")
	}
}

func TestUnavailableRulesAreRetriedWithoutActing(t *testing.T) {
	h := newCommentHarness(t)
	h.claims.err = errors.New("database down")
	h.rules.rules = []*rules.Rule{{ID: "r1", WorkspaceID: h.account.WorkspaceID, AccountID: h.account.ID, Source: shared.EntryTypeTikTok, Name: "Todos", Enabled: true, Match: rules.MatchAny, Actions: []rules.Action{rules.ActionHide}}}
	c := queued(h, "500", "oi")
	h.svc.lookup["500"] = ttdomain.RemoteComment{CommentID: "500", VideoID: c.VideoID, UniqueIdentifier: "+x==", Text: "oi"}
	h.comments.leased = []*ttdomain.Comment{c}
	_ = h.worker.Execute(context.Background())
	if len(h.svc.hidden) != 0 || h.comments.result["row-500"] != "deferred" {
		t.Fatalf("hidden %v result %v", h.svc.hidden, h.comments.result)
	}
}

func TestAudienceReadsContextAndBackfillsPages(t *testing.T) {
	h := newCommentHarness(t)
	posted := testNow.Add(-time.Hour)
	_ = h.videos.UpsertMany(context.Background(), []*ttdomain.Video{{AccountID: h.account.ID, WorkspaceID: h.account.WorkspaceID, ItemID: "7203946942097902849", Caption: "Promo", ShareURL: "https://www.tiktok.com/@loja/video/1", PostedAt: &posted, Comments: 7}})
	ref := ca.ContainerRef{Source: ca.SourceTikTok, AccountID: h.account.ID, ContainerID: "7203946942097902849"}
	ctxInfo, err := h.audience.ReadContainerContext(context.Background(), ref)
	if err != nil || ctxInfo.Caption != "Promo" || ctxInfo.AccountName != "Loja Oficial" {
		t.Fatalf("context %+v err %v", ctxInfo, err)
	}
	h.svc.commentPage = ttdomain.CommentPage{Comments: []ttdomain.RemoteComment{{CommentID: "600", VideoID: ref.ContainerID, UniqueIdentifier: "+c==", Username: "carla", Text: "Amei", Visibility: ttdomain.VisibilityPublic}}, HasMore: true, Cursor: 30}
	items, next, err := h.audience.FetchCommentsPage(context.Background(), ref, "")
	if err != nil || len(items) != 1 || next != "30" || items[0].AuthorHandle != "carla" {
		t.Fatalf("items %+v next %q err %v", items, next, err)
	}
	if _, _, err := h.audience.FetchCommentsPage(context.Background(), ref, "abc"); err == nil {
		t.Error("a malformed cursor must be refused")
	}
	containers, err := h.audience.ListContainers(context.Background(), h.account.ID, 10, 0)
	if err != nil || len(containers) != 1 || containers[0].CommentsCount != 7 {
		t.Fatalf("containers %+v err %v", containers, err)
	}
}
