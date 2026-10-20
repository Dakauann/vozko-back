package mercadolivre_repository

import (
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

	mldomain "vozko/domain/mercadolivre"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

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
	schemaName := "ml_test_" + uuid.New().String()[:8]
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
	if err := db.AutoMigrate(&schema.MercadoLivreAccount{}, &schema.MercadoLivreContact{}, &schema.MercadoLivreConversation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{
		"ux_ml_account_live_user", "ux_ml_contact_account_user", "ux_ml_conversation_account_pack",
		"idx_ml_acct_refresh_due", "idx_ml_acct_reconcile_due", "idx_ml_acct_health_due", "idx_ml_acct_purge_due",
		"idx_ml_conv_workspace_lastmsg", "idx_ml_conv_account_lastmsg", "idx_ml_acct_workspace_created",
		"idx_ml_conv_account_created", "idx_ml_conv_account_shipment", "idx_ml_contact_questions_blocked",
	} {
		stmt, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

type fixture struct {
	accounts      mldomain.AccountRepository
	contacts      mldomain.ContactRepository
	conversations mldomain.ConversationRepository
	account       *mldomain.Account
	contact       *mldomain.Contact
}

func newFixture(t *testing.T, db *gorm.DB) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{
		accounts:      NewAccountRepository(db),
		contacts:      NewContactRepository(db),
		conversations: NewConversationRepository(db),
	}
	expires := time.Now().UTC().Add(time.Hour)
	f.account = &mldomain.Account{
		WorkspaceID: uuid.NewString(), MLUserID: "415458330", SiteID: mldomain.SiteMLB, Status: mldomain.StatusConnected,
		AccessToken: "APP_USR-1", RefreshToken: "TG-1", AccessExpiresAt: &expires, GrantedScopes: mldomain.RequiredScopes(),
	}
	if err := f.accounts.Create(ctx, f.account); err != nil {
		t.Fatal(err)
	}
	contact, err := f.contacts.FindOrCreateByUserID(ctx, mldomain.FindOrCreateContactInput{
		WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, MLUserID: "123456789", Nickname: "COMPRADOR",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.contact = contact
	return f
}

func TestConcurrentClaimsAtBudgetOneLetExactlyOneSend(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	conv, _, err := f.conversations.FindOrCreateByPack(ctx, mldomain.FindOrCreateConversationInput{
		WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ContactID: f.contact.ID, PackKey: "2000000089077943", BuyerMLUserID: "123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
	mediated := true
	if err := f.conversations.ApplyStatus(ctx, conv.ID, mldomain.ThreadState{
		Status: mldomain.ThreadStatusActive, AgentMediated: &mediated, OutboundBudget: 1, WindowOpen: true, ReadAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	const racers = 16
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := f.conversations.ClaimOutbound(ctx, conv.ID)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if claimed {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d claims won at budget 1", wins.Load())
	}

	if err := f.conversations.ReleaseOutbound(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.conversations.ReleaseOutbound(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.conversations.FindByID(ctx, conv.ID)
	if got.OutboundSinceInbound != 0 {
		t.Fatalf("release went below zero: %d", got.OutboundSinceInbound)
	}

	claimed, _ := f.conversations.ClaimOutbound(ctx, conv.ID)
	if !claimed {
		t.Fatal("a released claim must be claimable again")
	}
	inbound := time.Now().UTC()
	if err := f.conversations.RecordInbound(ctx, conv.ID, inbound); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.conversations.ClaimOutbound(ctx, conv.ID); !again {
		t.Fatal("a new inbound message must reset the budget")
	}
	if err := f.conversations.RecordInbound(ctx, conv.ID, inbound.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if stale, _ := f.conversations.ClaimOutbound(ctx, conv.ID); stale {
		t.Fatal("an older inbound delivered late must not reset the budget")
	}

	if err := f.conversations.ApplyStatus(ctx, conv.ID, mldomain.ThreadState{
		Status: mldomain.ThreadStatusBlocked, Substatus: "blocked_by_mediation", AgentMediated: &mediated, OutboundBudget: 1, WindowOpen: false, ReadAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.conversations.RecordInbound(ctx, conv.ID, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if closed, _ := f.conversations.ClaimOutbound(ctx, conv.ID); closed {
		t.Fatal("a closed window must refuse every claim")
	}
}

func TestConcurrentFindOrCreateByPackCreatesOneRow(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := newFixture(t, db)

	const racers = 12
	var created atomic.Int32
	ids := make(chan string, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conv, made, err := f.conversations.FindOrCreateByPack(ctx, mldomain.FindOrCreateConversationInput{
				WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ContactID: f.contact.ID, PackKey: "2000000089077943",
			})
			if err != nil {
				t.Errorf("find or create: %v", err)
				return
			}
			if made {
				created.Add(1)
			}
			ids <- conv.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	var rows int64
	db.Model(&schema.MercadoLivreConversation{}).Where("account_id = ?", f.account.ID).Count(&rows)
	if rows != 1 || len(seen) != 1 || created.Load() != 1 {
		t.Fatalf("rows=%d ids=%d created=%d", rows, len(seen), created.Load())
	}

	var contacts atomic.Int32
	var cwg sync.WaitGroup
	for i := 0; i < racers; i++ {
		cwg.Add(1)
		go func() {
			defer cwg.Done()
			c, err := f.contacts.FindOrCreateByUserID(ctx, mldomain.FindOrCreateContactInput{
				WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, MLUserID: "987654321",
			})
			if err == nil && c.ID != "" {
				contacts.Add(1)
			}
		}()
	}
	cwg.Wait()
	var contactRows int64
	db.Model(&schema.MercadoLivreContact{}).Where("account_id = ? AND ml_user_id = ?", f.account.ID, "987654321").Count(&contactRows)
	if contactRows != 1 || contacts.Load() != racers {
		t.Fatalf("contact rows=%d resolved=%d", contactRows, contacts.Load())
	}
}

func TestLockForRefreshIsSingleFlight(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := newFixture(t, db)

	locked := make(chan struct{})
	release := make(chan struct{})
	var first sync.WaitGroup
	first.Add(1)
	var firstErr error
	var firstAcquired bool
	go func() {
		defer first.Done()
		firstAcquired, firstErr = f.accounts.LockForRefresh(ctx, f.account.ID, func(_ context.Context, a *mldomain.Account) (mldomain.RefreshOutcome, error) {
			close(locked)
			<-release
			expires := time.Now().UTC().Add(6 * time.Hour)
			return mldomain.RefreshOutcome{Tokens: &mldomain.TokenUpdate{
				AccessToken: "APP_USR-2", RefreshToken: "TG-2", AccessExpiresAt: expires, RefreshExpiresAt: expires.Add(time.Hour),
			}}, nil
		})
	}()
	<-locked

	secondCalled := false
	acquired, err := f.accounts.LockForRefresh(ctx, f.account.ID, func(context.Context, *mldomain.Account) (mldomain.RefreshOutcome, error) {
		secondCalled = true
		return mldomain.RefreshOutcome{}, nil
	})
	close(release)
	first.Wait()
	if err != nil || acquired || secondCalled {
		t.Fatalf("second refresher acquired=%v called=%v err=%v", acquired, secondCalled, err)
	}
	if firstErr != nil || !firstAcquired {
		t.Fatalf("first refresher acquired=%v err=%v", firstAcquired, firstErr)
	}
	got, err := f.accounts.FindByID(ctx, f.account.ID)
	if err != nil || got.AccessToken != "APP_USR-2" || got.RefreshToken != "TG-2" {
		t.Fatalf("rotated pair not stored: %+v %v", got, err)
	}

	if _, err := f.accounts.LockForRefresh(ctx, f.account.ID, func(context.Context, *mldomain.Account) (mldomain.RefreshOutcome, error) {
		return mldomain.RefreshOutcome{Status: mldomain.StatusNeedsReconnect, StatusReason: "invalid_grant"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	dead, _ := f.accounts.FindByID(ctx, f.account.ID)
	if dead.Status != mldomain.StatusNeedsReconnect || dead.AccessToken != "" || dead.RefreshToken != "" {
		t.Fatalf("a dead grant must be wiped: %+v", dead)
	}
	due, err := f.accounts.ListDueForRefresh(ctx, time.Now().UTC().Add(24*time.Hour), 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("a dead grant is never retried: %v %v", due, err)
	}
}

func TestDisconnectFreesTheSellerAndReconnectRestoresIt(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := newFixture(t, db)

	other := &mldomain.Account{WorkspaceID: uuid.NewString(), MLUserID: f.account.MLUserID, SiteID: mldomain.SiteMLB, Status: mldomain.StatusConnected, AccessToken: "x", RefreshToken: "y"}
	if err := f.accounts.Create(ctx, other); err == nil {
		t.Fatal("a seller live in one workspace must not connect to another")
	}

	purge := time.Now().UTC().Add(-time.Minute)
	if err := f.accounts.Disconnect(ctx, f.account.ID, mldomain.StatusDisconnected, "test", purge); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.FindByID(ctx, f.account.ID); err == nil {
		t.Fatal("a disconnected account is not live")
	}
	due, err := f.accounts.ListDueForPurge(ctx, time.Now().UTC(), 10)
	if err != nil || len(due) != 1 || due[0].AccessToken != "" {
		t.Fatalf("purge due = %v %v", due, err)
	}

	restored, err := f.accounts.FindByWorkspaceAndMLUserIDUnscoped(ctx, f.account.WorkspaceID, f.account.MLUserID)
	if err != nil {
		t.Fatal(err)
	}
	restored.AccessToken, restored.RefreshToken, restored.Status = "APP_USR-9", "TG-9", mldomain.StatusConnected
	if err := f.accounts.Reconnect(ctx, restored); err != nil {
		t.Fatal(err)
	}
	live, err := f.accounts.FindLiveByMLUserID(ctx, f.account.MLUserID)
	if err != nil || live.ID != f.account.ID || live.PurgeAfter != nil || live.AccessToken != "APP_USR-9" {
		t.Fatalf("reconnect = %+v %v", live, err)
	}
}

func TestTheQuestionBlockMirrorConvergesOnTheBlacklist(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	contact := func(userID string) *mldomain.Contact {
		c, err := f.contacts.FindOrCreateByUserID(ctx, mldomain.FindOrCreateContactInput{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, MLUserID: userID})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	blocked := func(c *mldomain.Contact) bool {
		got, err := f.contacts.FindByID(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.QuestionsBlocked()
	}
	viaVozko, external, unblockedOutside, stranger := f.contact, contact("222"), contact("333"), contact("444")
	earlier := time.Now().UTC().Add(-time.Hour)
	if err := f.contacts.SetQuestionsBlocked(ctx, viaVozko.ID, &earlier); err != nil {
		t.Fatal(err)
	}
	if err := f.contacts.SetQuestionsBlocked(ctx, unblockedOutside.ID, &earlier); err != nil {
		t.Fatal(err)
	}

	scanStart := time.Now().UTC().Add(time.Second)
	list := []string{viaVozko.MLUserID, external.MLUserID, "999999"}
	if err := f.contacts.MarkQuestionsBlocked(ctx, f.account.ID, list, scanStart, scanStart); err != nil {
		t.Fatal(err)
	}
	if err := f.contacts.ClearQuestionsBlockedExcept(ctx, f.account.ID, list, scanStart); err != nil {
		t.Fatal(err)
	}
	if !blocked(viaVozko) || !blocked(external) || blocked(unblockedOutside) || blocked(stranger) {
		t.Fatalf("vozko=%v external=%v unblockedOutside=%v stranger=%v", blocked(viaVozko), blocked(external), blocked(unblockedOutside), blocked(stranger))
	}
	again, err := f.contacts.FindByID(ctx, viaVozko.ID)
	if err != nil || again.QuestionsBlockedAt == nil || !again.QuestionsBlockedAt.Before(scanStart) {
		t.Fatalf("a block already mirrored keeps its time: %+v %v", again, err)
	}

	duringScan := time.Now().UTC().Add(-time.Minute)
	if err := f.contacts.SetQuestionsBlocked(ctx, stranger.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.contacts.MarkQuestionsBlocked(ctx, f.account.ID, []string{stranger.MLUserID}, time.Now().UTC(), duringScan); err != nil {
		t.Fatal(err)
	}
	if blocked(stranger) {
		t.Fatal("a contact changed after the scan began must not be marked from that scan")
	}
	fresh := time.Now().UTC()
	if err := f.contacts.SetQuestionsBlocked(ctx, stranger.ID, &fresh); err != nil {
		t.Fatal(err)
	}
	if err := f.contacts.ClearQuestionsBlockedExcept(ctx, f.account.ID, nil, duringScan); err != nil {
		t.Fatal(err)
	}
	if !blocked(stranger) {
		t.Fatal("a block made after the scan began must survive the clear")
	}
	if err := f.contacts.ClearQuestionsBlockedExcept(ctx, f.account.ID, nil, time.Now().UTC().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if blocked(viaVozko) || blocked(external) || blocked(stranger) {
		t.Fatal("an empty complete blacklist clears every older block of the account")
	}
	if err := f.contacts.SetQuestionsBlocked(ctx, "00000000-0000-0000-0000-000000000000", &fresh); !errors.Is(err, mldomain.ErrContactNotFound) {
		t.Fatalf("missing contact = %v", err)
	}
}
