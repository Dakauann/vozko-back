package tiktok_repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	ttdomain "vozko/domain/tiktok"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

func withPII(t *testing.T) {
	t.Helper()
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(s)
	t.Cleanup(func() { piigorm.SetService(nil) })
}

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	withPII(t)
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "tt_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(schema.TikTokModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range database.TikTokDDLStatements() {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("ddl %s: %v", stmt, err)
		}
	}
	return db
}

type fixture struct {
	db            *gorm.DB
	accounts      ttdomain.AccountRepository
	contacts      ttdomain.ContactRepository
	conversations ttdomain.ConversationRepository
	comments      ttdomain.CommentRepository
	videos        ttdomain.VideoRepository
	account       *ttdomain.Account
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := integrationDB(t)
	f := fixture{
		db:            db,
		accounts:      NewAccountRepository(db),
		contacts:      NewContactRepository(db),
		conversations: NewConversationRepository(db),
		comments:      NewCommentRepository(db),
		videos:        NewVideoRepository(db),
	}
	expires := time.Now().UTC().Add(20 * time.Hour)
	f.account = &ttdomain.Account{
		WorkspaceID: uuid.NewString(), OpenID: "_000" + uuid.NewString()[:8], Status: ttdomain.StatusConnected,
		AccessToken: "act.1", RefreshToken: "rft.1", AccessExpiresAt: &expires,
		GrantedScopes: []string{ttdomain.ScopeVideoList, ttdomain.ScopeCommentList},
		Capabilities:  ttdomain.AccountCapabilities{CanReadVideos: true, CanReadComments: true},
	}
	if err := f.accounts.Create(context.Background(), f.account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return f
}

func (f fixture) conversation(t *testing.T, conversationID string) *ttdomain.Conversation {
	t.Helper()
	ctx := context.Background()
	contact, err := f.contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, UniqueIdentifier: "+person/" + conversationID})
	if err != nil {
		t.Fatalf("contact: %v", err)
	}
	conv, _, err := f.conversations.FindOrCreate(ctx, ttdomain.FindOrCreateConversationInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ContactID: contact.ID, ConversationID: conversationID})
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	return conv
}

func TestAccountTokensAreEncryptedAndTheOpenIDIsUniqueWhileLive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var raw []byte
	if err := f.db.Raw("SELECT access_token FROM tiktok_accounts WHERE id = ?", f.account.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("act.1")) {
		t.Fatal("the access token is stored in clear text")
	}
	got, err := f.accounts.FindLiveByOpenID(ctx, f.account.OpenID)
	if err != nil || got.AccessToken != "act.1" || !got.Capabilities.CanReadComments {
		t.Fatalf("got %+v err %v", got, err)
	}
	dup := *f.account
	dup.ID = ""
	dup.WorkspaceID = uuid.NewString()
	if err := f.accounts.Create(ctx, &dup); !errors.Is(err, ttdomain.ErrAccountAlreadyLinked) {
		t.Fatalf("second live account with the same open id: %v", err)
	}
}

func TestDisconnectWipesTokensAndSchedulesThePurge(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	purgeAt := time.Now().UTC().Add(-time.Minute)
	if err := f.accounts.Disconnect(ctx, f.account.ID, ttdomain.StatusDisconnected, "operator", purgeAt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.FindLiveByOpenID(ctx, f.account.OpenID); !errors.Is(err, ttdomain.ErrAccountNotFound) {
		t.Fatalf("a disconnected account must not receive events: %v", err)
	}
	due, err := f.accounts.ListDueForPurge(ctx, time.Now().UTC(), 10)
	if err != nil || len(due) != 1 || due[0].AccessToken != "" || due[0].RefreshToken != "" {
		t.Fatalf("due %+v err %v", due, err)
	}
	if err := f.accounts.Restore(ctx, f.account.ID); err != nil {
		t.Fatal(err)
	}
	if due, _ := f.accounts.ListDueForPurge(ctx, time.Now().UTC(), 10); len(due) != 0 {
		t.Fatal("restoring must cancel the purge")
	}
}

func TestLockForRefreshIsSingleFlight(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = f.accounts.LockForRefresh(ctx, f.account.ID, func(ctx context.Context, a *ttdomain.Account) (ttdomain.RefreshOutcome, error) {
			atomic.AddInt32(&calls, 1)
			close(started)
			<-release
			return ttdomain.RefreshOutcome{Tokens: &ttdomain.TokenUpdate{AccessToken: "act.2", RefreshToken: "rft.2", AccessExpiresAt: time.Now().Add(24 * time.Hour), RefreshExpiresAt: time.Now().Add(365 * 24 * time.Hour)}}, nil
		})
	}()
	<-started
	acquired, err := f.accounts.LockForRefresh(ctx, f.account.ID, func(ctx context.Context, a *ttdomain.Account) (ttdomain.RefreshOutcome, error) {
		atomic.AddInt32(&calls, 1)
		return ttdomain.RefreshOutcome{}, nil
	})
	close(release)
	wg.Wait()
	if err != nil || acquired {
		t.Fatalf("a second refresh must skip the locked row: acquired %v err %v", acquired, err)
	}
	if calls != 1 {
		t.Fatalf("refresh ran %d times", calls)
	}
	got, _ := f.accounts.FindByID(ctx, f.account.ID)
	if got.AccessToken != "act.2" || got.RefreshToken != "rft.2" {
		t.Fatalf("tokens not stored as a pair: %+v", got)
	}
}

func TestARefusedRefreshWipesTheTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.accounts.LockForRefresh(ctx, f.account.ID, func(ctx context.Context, a *ttdomain.Account) (ttdomain.RefreshOutcome, error) {
		return ttdomain.RefreshOutcome{Status: ttdomain.StatusNeedsReconnect, StatusReason: "refresh refused"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.accounts.FindByID(ctx, f.account.ID)
	if got.Status != ttdomain.StatusNeedsReconnect || got.AccessToken != "" || got.RefreshToken != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestContactFindOrCreateIsRaceSafe(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := f.contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, UniqueIdentifier: "+ABc/same==", Username: "ana"})
			if err != nil {
				t.Errorf("find or create: %v", err)
				return
			}
			ids <- c.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent deliveries created %d contacts", len(seen))
	}
}

func TestTheBudgetNeverOvershootsUnderConcurrentSends(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conv := f.conversation(t, "S:1:budget")
	now := time.Now().UTC()
	if err := f.conversations.RecordInbound(ctx, conv.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	var claimed int32
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := f.conversations.ClaimOutbound(ctx, conv.ID, now)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			if ok {
				atomic.AddInt32(&claimed, 1)
			}
		}()
	}
	wg.Wait()
	if claimed != ttdomain.OutboundBudget {
		t.Fatalf("claimed %d sends, budget is %d", claimed, ttdomain.OutboundBudget)
	}
	if err := f.conversations.ReleaseOutbound(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.conversations.ClaimOutbound(ctx, conv.ID, now); !ok {
		t.Fatal("a released claim frees one send")
	}
	if err := f.conversations.RecordInbound(ctx, conv.ID, now); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.conversations.ClaimOutbound(ctx, conv.ID, now); !ok {
		t.Fatal("a new customer message resets the budget")
	}
	got, _ := f.conversations.FindByID(ctx, conv.ID)
	if got.OutboundSinceInbound != 1 || got.Window(now).Open != true {
		t.Fatalf("after the reset: %+v", got)
	}
}

func TestClaimsRefuseExpiredOrClosedWindows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	never := f.conversation(t, "S:1:never")
	if ok, _ := f.conversations.ClaimOutbound(ctx, never.ID, now); ok {
		t.Fatal("a conversation the customer never wrote in cannot send")
	}
	old := f.conversation(t, "S:1:old")
	_ = f.conversations.RecordInbound(ctx, old.ID, now.Add(-49*time.Hour))
	if ok, _ := f.conversations.ClaimOutbound(ctx, old.ID, now); ok {
		t.Fatal("48h after the customer, sends are refused")
	}
	closed := f.conversation(t, "S:1:closed")
	_ = f.conversations.RecordInbound(ctx, closed.ID, now.Add(-time.Hour))
	if err := f.conversations.CloseBudget(ctx, closed.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.conversations.ClaimOutbound(ctx, closed.ID, now); ok {
		t.Fatal("after tiktok closed the conversation, sends are refused")
	}
	_ = f.conversations.RecordInbound(ctx, closed.ID, now)
	if ok, _ := f.conversations.ClaimOutbound(ctx, closed.ID, now.Add(time.Second)); !ok {
		t.Fatal("a customer message after the close reopens it")
	}
}

func TestEchoesFromTheAppCountAgainstTheBudget(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	conv := f.conversation(t, "S:1:echo")
	_ = f.conversations.RecordInbound(ctx, conv.ID, now.Add(-time.Minute))
	for i := 0; i < ttdomain.OutboundBudget; i++ {
		if err := f.conversations.CountOutbound(ctx, conv.ID); err != nil {
			t.Fatal(err)
		}
	}
	if ok, _ := f.conversations.ClaimOutbound(ctx, conv.ID, now); ok {
		t.Fatal("ten messages from the app spend the budget")
	}
}

func TestConversationFindOrCreateReportsCreation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	contact, _ := f.contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, UniqueIdentifier: "+x=="})
	in := ttdomain.FindOrCreateConversationInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ContactID: contact.ID, ConversationID: "S:1:abc+def/ghi="}
	first, created, err := f.conversations.FindOrCreate(ctx, in)
	if err != nil || !created {
		t.Fatalf("first: created %v err %v", created, err)
	}
	second, created, err := f.conversations.FindOrCreate(ctx, in)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second: created %v err %v", created, err)
	}
	if err := f.conversations.SetReferral(ctx, first.ID, ttdomain.Referral{Source: "ad", AdID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := f.conversations.SetImageCapability(ctx, first.ID, true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, _ := f.conversations.FindByID(ctx, first.ID)
	if got.Referral == nil || got.Referral.AdID != "1" || got.ImageSendAllowed == nil || !*got.ImageSendAllowed {
		t.Fatalf("got %+v", got)
	}
	dept, err := f.conversations.DepartmentIDForEntry(ctx, first.ID)
	if err != nil || dept != "" {
		t.Fatalf("department %q err %v", dept, err)
	}
}

func insertEvent(id string, at time.Time) ttdomain.CommentEventWrite {
	return ttdomain.CommentEventWrite{
		Event:      ttdomain.CommentEvent{CommentID: id, VideoID: "7203946942097902849", Kind: ttdomain.KindComment, Action: ttdomain.CommentInserted, PersonID: "+p==", Text: "Qual o preço?"},
		OccurredAt: at,
	}
}

func TestCommentEventsApplyInOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)
	w := insertEvent("7247303576418566913", at)
	w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
	if applied, err := f.comments.ApplyEvent(ctx, w); err != nil || !applied {
		t.Fatalf("insert: applied %v err %v", applied, err)
	}
	if applied, _ := f.comments.ApplyEvent(ctx, w); applied {
		t.Fatal("a redelivered event must not apply twice")
	}
	older := w
	older.Event.Action = ttdomain.CommentSetHidden
	older.OccurredAt = at.Add(-time.Second)
	if applied, _ := f.comments.ApplyEvent(ctx, older); applied {
		t.Fatal("an older event must not overwrite a newer one")
	}
	got, _ := f.comments.FindByCommentID(ctx, f.account.ID, "7247303576418566913")
	if got.Visibility != ttdomain.VisibilityPublic || !got.RulesDue || got.Lookup != ttdomain.LookupPending || got.Text != "Qual o preço?" {
		t.Fatalf("after insert: %+v", got)
	}
	deleted := w
	deleted.Event.Action = ttdomain.CommentDeleted
	deleted.Event.Text = ""
	deleted.OccurredAt = at.Add(time.Second)
	if applied, _ := f.comments.ApplyEvent(ctx, deleted); !applied {
		t.Fatal("a newer delete applies")
	}
	got, _ = f.comments.FindByCommentID(ctx, f.account.ID, "7247303576418566913")
	if got.Visibility != ttdomain.VisibilityDeleted || got.RulesDue || got.Text != "Qual o preço?" {
		t.Fatalf("after delete: %+v", got)
	}
}

func TestOurOwnCommentsNeverQueueRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.comments.RecordOurs(ctx, &ttdomain.Comment{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, VideoID: "7203946942097902849", CommentID: "900", ParentCommentID: "800", Text: "Obrigado!"}); err != nil {
		t.Fatal(err)
	}
	w := insertEvent("900", time.Now().UTC())
	w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
	if _, err := f.comments.ApplyEvent(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ := f.comments.FindByCommentID(ctx, f.account.ID, "900")
	if got.RulesDue || got.IsOurs == nil || !*got.IsOurs {
		t.Fatalf("our reply queued rules: %+v", got)
	}
}

func TestMirroredCommentsDoNotQueueRulesButALaterInsertDoes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	no := false
	if err := f.comments.MirrorMany(ctx, []*ttdomain.Comment{{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, VideoID: "7203946942097902849", CommentID: "901", Text: "oi", Visibility: ttdomain.VisibilityPublic, IsOurs: &no, AuthorUsername: "ana"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.comments.FindByCommentID(ctx, f.account.ID, "901")
	if got.RulesDue || got.Lookup != ttdomain.LookupResolved {
		t.Fatalf("mirror queued work: %+v", got)
	}
	w := insertEvent("901", time.Now().UTC())
	w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
	if _, err := f.comments.ApplyEvent(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ = f.comments.FindByCommentID(ctx, f.account.ID, "901")
	if !got.RulesDue || got.Lookup != ttdomain.LookupResolved || got.AuthorUsername != "ana" {
		t.Fatalf("a webhook insert for a mirrored comment must queue its rules: %+v", got)
	}
}

func TestWorkClaimsLeaseRowsSoParallelWorkersNeverShareThem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 30; i++ {
		w := insertEvent(fmt.Sprintf("%d", 1000+i), now.Add(-time.Minute))
		w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
		if _, err := f.comments.ApplyEvent(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.comments.ClaimLookups(ctx, now, time.Minute, 15)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			mu.Lock()
			for _, c := range got {
				seen[c.ID]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != 30 {
		t.Fatalf("claimed %d distinct rows, want 30", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("row %s claimed %d times", id, n)
		}
	}
	if again, _ := f.comments.ClaimLookups(ctx, now, time.Minute, 50); len(again) != 0 {
		t.Fatalf("leased rows were claimed again: %d", len(again))
	}
	if later, _ := f.comments.ClaimLookups(ctx, now.Add(2*time.Minute), time.Minute, 50); len(later) != 30 {
		t.Fatalf("expired leases must be claimable again, got %d", len(later))
	}
}

func TestResolveDeferFailAndMarkRulesDone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	w := insertEvent("2000", now)
	w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
	_, _ = f.comments.ApplyEvent(ctx, w)
	c, _ := f.comments.FindByCommentID(ctx, f.account.ID, "2000")
	if err := f.comments.DeferLookup(ctx, c.ID, 3, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if due, _ := f.comments.ClaimLookups(ctx, now, time.Minute, 10); len(due) != 0 {
		t.Fatal("a deferred lookup waits for its time")
	}
	if err := f.comments.ResolveLookup(ctx, c.ID, ttdomain.ResolvedAuthor{Username: "ana", DisplayName: "Ana", Visibility: ttdomain.VisibilityPublic}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.comments.FindByCommentID(ctx, f.account.ID, "2000")
	if got.Lookup != ttdomain.LookupResolved || got.IsOurs == nil || *got.IsOurs || !got.RulesDue || got.AuthorUsername != "ana" || !got.ReadyForRules() {
		t.Fatalf("resolved: %+v", got)
	}
	if err := f.comments.MarkRulesDone(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if due, _ := f.comments.ClaimLookups(ctx, now.Add(2*time.Hour), time.Minute, 10); len(due) != 0 {
		t.Fatal("finished work must leave the queue")
	}
	w2 := insertEvent("2001", now)
	w2.WorkspaceID, w2.AccountID = f.account.WorkspaceID, f.account.ID
	_, _ = f.comments.ApplyEvent(ctx, w2)
	c2, _ := f.comments.FindByCommentID(ctx, f.account.ID, "2001")
	if err := f.comments.FailLookup(ctx, c2.ID, ttdomain.MaxLookupAttempts); err != nil {
		t.Fatal(err)
	}
	got2, _ := f.comments.FindByCommentID(ctx, f.account.ID, "2001")
	if got2.Lookup != ttdomain.LookupFailed || got2.RulesDue {
		t.Fatalf("failed: %+v", got2)
	}
}

func TestCommentListHidesDeletedAndOptionallyHidden(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i, action := range []ttdomain.CommentAction{ttdomain.CommentInserted, ttdomain.CommentSetHidden, ttdomain.CommentDeleted} {
		w := insertEvent(fmt.Sprintf("%d", 3000+i), now)
		w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
		w.Event.Action = action
		if _, err := f.comments.ApplyEvent(ctx, w); err != nil {
			t.Fatal(err)
		}
	}
	visible, err := f.comments.List(ctx, ttdomain.ListCommentsInput{AccountID: f.account.ID})
	if err != nil || visible.TotalItems != 1 {
		t.Fatalf("visible %+v err %v", visible, err)
	}
	all, _ := f.comments.List(ctx, ttdomain.ListCommentsInput{AccountID: f.account.ID, IncludeHidden: true})
	if all.TotalItems != 2 {
		t.Fatalf("with hidden: %d", all.TotalItems)
	}
}

func TestVideoUpsertKeepsTheThumbnailWhenTheNewOneIsMissing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	posted := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	v := &ttdomain.Video{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: "7044071604573310209", Caption: "Promo", ThumbnailKey: "tiktok/thumb.jpg", PostedAt: &posted, Comments: 3}
	if err := f.videos.UpsertMany(ctx, []*ttdomain.Video{v}); err != nil {
		t.Fatal(err)
	}
	v2 := *v
	v2.ThumbnailKey = ""
	v2.Comments = 9
	if err := f.videos.UpsertMany(ctx, []*ttdomain.Video{&v2}); err != nil {
		t.Fatal(err)
	}
	got, err := f.videos.FindByItemID(ctx, f.account.ID, "7044071604573310209")
	if err != nil || got.ThumbnailKey != "tiktok/thumb.jpg" || got.Comments != 9 || got.PostedAt == nil {
		t.Fatalf("got %+v err %v", got, err)
	}
	page, err := f.videos.ListByAccount(ctx, ttdomain.ListVideosInput{AccountID: f.account.ID, Search: "pro"})
	if err != nil || page.TotalItems != 1 {
		t.Fatalf("page %+v err %v", page, err)
	}
}

func TestPurgeRemovesEveryRowOfTheAccountInBatches(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	purge := NewPurgeRepository(f.db)
	for i := 0; i < 5; i++ {
		f.conversation(t, fmt.Sprintf("S:1:purge-%d", i))
	}
	contact, _ := f.contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, UniqueIdentifier: "+avatar=="})
	_ = f.contacts.UpdateProfile(ctx, contact.ID, ttdomain.ContactProfile{AvatarKey: "tiktok/avatars/a.jpg"}, time.Now().UTC())
	_ = f.videos.UpsertMany(ctx, []*ttdomain.Video{{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: "1", ThumbnailKey: "tiktok/thumbs/1.jpg"}})
	for i := 0; i < 3; i++ {
		w := insertEvent(fmt.Sprintf("%d", 5000+i), time.Now().UTC())
		w.WorkspaceID, w.AccountID = f.account.WorkspaceID, f.account.ID
		_, _ = f.comments.ApplyEvent(ctx, w)
	}
	other := newFixtureAccount(t, f)
	otherConv := f.conversationFor(t, other, "S:1:keep")

	keys, err := purge.StoredObjectKeys(ctx, f.account.ID, 100)
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys %v err %v", keys, err)
	}
	if err := purge.ForgetObjectKeys(ctx, f.account.ID, keys); err != nil {
		t.Fatal(err)
	}
	if again, _ := purge.StoredObjectKeys(ctx, f.account.ID, 100); len(again) != 0 {
		t.Fatalf("forgotten keys came back: %v", again)
	}
	for {
		ids, err := purge.ConversationIDsForPurge(ctx, f.account.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			break
		}
		if err := purge.DeleteConversations(ctx, f.account.ID, ids); err != nil {
			t.Fatal(err)
		}
	}
	for {
		done, err := purge.DeleteAccountRows(ctx, f.account.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	for {
		ids, err := purge.ContactIDsForPurge(ctx, f.account.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			break
		}
		if err := purge.DeleteContacts(ctx, f.account.ID, ids); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"tiktok_conversations", "tiktok_contacts", "tiktok_videos", "tiktok_comments"} {
		var n int64
		_ = f.db.Raw("SELECT count(*) FROM "+table+" WHERE account_id = ?", f.account.ID).Scan(&n).Error
		if n != 0 {
			t.Errorf("%s kept %d rows of the purged account", table, n)
		}
	}
	if _, err := f.conversations.FindByID(ctx, otherConv.ID); err != nil {
		t.Fatalf("another account's conversation was purged: %v", err)
	}
}

func newFixtureAccount(t *testing.T, f fixture) *ttdomain.Account {
	t.Helper()
	a := &ttdomain.Account{WorkspaceID: f.account.WorkspaceID, OpenID: "_000" + uuid.NewString()[:8], Status: ttdomain.StatusConnected}
	if err := f.accounts.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (f fixture) conversationFor(t *testing.T, a *ttdomain.Account, conversationID string) *ttdomain.Conversation {
	t.Helper()
	ctx := context.Background()
	contact, err := f.contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: a.WorkspaceID, AccountID: a.ID, UniqueIdentifier: "+other/" + conversationID})
	if err != nil {
		t.Fatal(err)
	}
	conv, _, err := f.conversations.FindOrCreate(ctx, ttdomain.FindOrCreateConversationInput{WorkspaceID: a.WorkspaceID, AccountID: a.ID, ContactID: contact.ID, ConversationID: conversationID})
	if err != nil {
		t.Fatal(err)
	}
	return conv
}
