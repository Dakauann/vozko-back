package mercadolivre_repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	mldomain "vozko/domain/mercadolivre"
	"vozko/infra/database/schema"
)

func catalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := integrationDB(t)
	if err := db.AutoMigrate(&schema.MercadoLivreListing{}, &schema.MercadoLivreListingSync{}, &schema.MercadoLivreQuestion{}); err != nil {
		t.Fatalf("migrate catalog: %v", err)
	}
	return db
}

func openQuestion(f fixture, questionID string, asked time.Time) *mldomain.Question {
	return &mldomain.Question{
		WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: "MLB100", QuestionID: questionID, ContactID: f.contact.ID,
		Text: "Tem azul?", Status: mldomain.QuestionUnanswered, AskedAt: asked, LastFetchedAt: asked,
	}
}

func TestConcurrentUpsertsOfANewQuestionReportCreatedOnce(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	questions := NewQuestionRepository(db)
	now := time.Now().UTC()

	const racers = 10
	var created atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			made, err := questions.Upsert(ctx, openQuestion(f, "13000000001", now))
			if err != nil {
				t.Errorf("upsert: %v", err)
				return
			}
			if made {
				created.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	var rows int64
	db.Model(&schema.MercadoLivreQuestion{}).Where("account_id = ?", f.account.ID).Count(&rows)
	if created.Load() != 1 || rows != 1 {
		t.Fatalf("created=%d rows=%d", created.Load(), rows)
	}
}

func TestAnUpsertFollowsMercadoLivreButKeepsWhoAnswered(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	questions := NewQuestionRepository(db)
	asked := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	if made, err := questions.Upsert(ctx, openQuestion(f, "13000000001", asked)); err != nil || !made {
		t.Fatalf("first sight: %v %v", made, err)
	}
	operator := uuid.NewString()
	answeredAt := asked.Add(time.Minute)
	if err := questions.RecordAnswer(ctx, f.account.ID, "13000000001", mldomain.AnswerRecord{
		Text: "Tem sim", Status: mldomain.AnswerActive, AnsweredBy: mldomain.AnsweredByOperator, AnsweredByUserID: &operator, AnsweredAt: answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	echo := openQuestion(f, "13000000001", asked)
	echo.Status, echo.AnswerText, echo.AnswerStatus, echo.AnsweredBy = mldomain.QuestionAnswered, "Tem sim", mldomain.AnswerActive, mldomain.AnsweredByMercadoLivreApp
	echo.LastFetchedAt = asked.Add(2 * time.Minute)
	if made, err := questions.Upsert(ctx, echo); err != nil || made {
		t.Fatalf("echo: %v %v", made, err)
	}
	got, err := questions.FindByQuestionID(ctx, f.account.ID, "13000000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != mldomain.QuestionAnswered || got.AnsweredBy != mldomain.AnsweredByOperator || got.AnsweredByUserID == nil || *got.AnsweredByUserID != operator ||
		got.AnswerText != "Tem sim" || got.AnsweredAt == nil {
		t.Fatalf("question = %+v", got)
	}

	stale := openQuestion(f, "13000000001", asked)
	stale.LastFetchedAt = asked
	if _, err := questions.Upsert(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if again, _ := questions.FindByQuestionID(ctx, f.account.ID, "13000000001"); again.Status != mldomain.QuestionAnswered {
		t.Fatalf("an older read overwrote a newer one: %+v", again)
	}

	outside := openQuestion(f, "13000000002", asked)
	outside.Status, outside.AnswerText, outside.AnsweredBy = mldomain.QuestionAnswered, "Respondi no app", mldomain.AnsweredByMercadoLivreApp
	if _, err := questions.Upsert(ctx, outside); err != nil {
		t.Fatal(err)
	}
	if app, _ := questions.FindByQuestionID(ctx, f.account.ID, "13000000002"); app.AnsweredBy != mldomain.AnsweredByMercadoLivreApp {
		t.Fatalf("an answer given in the app = %+v", app)
	}

	if err := questions.SetStatus(ctx, f.account.ID, "13000000002", mldomain.QuestionDeleted); err != nil {
		t.Fatal(err)
	}
	if _, err := questions.FindByQuestionID(ctx, uuid.NewString(), "13000000002"); err != mldomain.ErrQuestionNotFound {
		t.Fatalf("another account found the question: %v", err)
	}
	if err := questions.SetStatus(ctx, f.account.ID, "99", mldomain.QuestionDeleted); err != mldomain.ErrQuestionNotFound {
		t.Fatalf("unknown question: %v", err)
	}
}

func TestQuestionsListUnansweredFirstWithKeysetPages(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	questions := NewQuestionRepository(db)
	base := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 7; i++ {
		q := openQuestion(f, fmt.Sprintf("1300000000%d", i), base.Add(time.Duration(i)*time.Minute))
		if i%2 == 1 {
			q.Status = mldomain.QuestionAnswered
		}
		if i == 6 {
			q.ItemID = "MLB200"
		}
		if _, err := questions.Upsert(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var order []string
	var after *mldomain.QuestionCursor
	for page := 0; page < 10; page++ {
		list, err := questions.List(ctx, mldomain.ListQuestionsInput{AccountID: f.account.ID, After: after, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range list.Items {
			order = append(order, q.QuestionID[len(q.QuestionID)-1:])
		}
		if list.Next == nil {
			break
		}
		after = list.Next
	}
	if got := fmt.Sprint(order); got != "[6 4 2 0 5 3 1]" {
		t.Fatalf("order = %s, want unanswered newest first then the rest", got)
	}

	answered := mldomain.QuestionAnswered
	list, err := questions.List(ctx, mldomain.ListQuestionsInput{AccountID: f.account.ID, Status: &answered, Limit: 2})
	if err != nil || len(list.Items) != 2 || list.Next == nil || list.Items[0].QuestionID != "13000000005" {
		t.Fatalf("answered page = %+v %v", list, err)
	}
	rest, err := questions.List(ctx, mldomain.ListQuestionsInput{AccountID: f.account.ID, Status: &answered, After: list.Next, Limit: 2})
	if err != nil || len(rest.Items) != 1 || rest.Next != nil || rest.Items[0].QuestionID != "13000000001" {
		t.Fatalf("answered rest = %+v %v", rest, err)
	}
	byItem, err := questions.List(ctx, mldomain.ListQuestionsInput{AccountID: f.account.ID, ItemID: "MLB200", Limit: 10})
	if err != nil || len(byItem.Items) != 1 || byItem.Items[0].QuestionID != "13000000006" {
		t.Fatalf("by item = %+v %v", byItem, err)
	}
	other, _ := questions.List(ctx, mldomain.ListQuestionsInput{AccountID: uuid.NewString(), Limit: 10})
	if len(other.Items) != 0 {
		t.Fatal("another account sees these questions")
	}
}

func TestListingsUpsertKeepTheirThumbnailAndPageByKeyset(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	listings := NewListingRepository(db)
	now := time.Now().UTC()

	var batch []*mldomain.Listing
	for i := 0; i < 5; i++ {
		synced := now
		batch = append(batch, &mldomain.Listing{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: fmt.Sprintf("MLB10%d", i),
			Title: fmt.Sprintf("Caneca %d", i), Status: "active", ThumbnailSource: "http://img", SyncedAt: &synced})
	}
	batch[4].Status = "paused"
	if err := listings.UpsertMany(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if err := listings.SetThumbnail(ctx, f.account.ID, "MLB100", "mercadolivre/k.jpg", "http://img"); err != nil {
		t.Fatal(err)
	}
	batch[0].Title, batch[0].ThumbnailSource = "Caneca nova", "http://img-2"
	if err := listings.UpsertMany(ctx, batch[:1]); err != nil {
		t.Fatal(err)
	}
	got, err := listings.FindByItemID(ctx, f.account.ID, "MLB100")
	if err != nil || got.Title != "Caneca nova" || got.ThumbnailKey != "mercadolivre/k.jpg" || got.ThumbnailSource != "http://img" {
		t.Fatalf("listing = %+v %v", got, err)
	}
	if _, err := listings.FindByItemID(ctx, uuid.NewString(), "MLB100"); err != mldomain.ErrListingNotFound {
		t.Fatalf("another account found the listing: %v", err)
	}
	found, err := listings.FindByItemIDs(ctx, f.account.ID, []string{"MLB101", "MLB102", "MLB999", "MLB101"})
	if err != nil || len(found) != 2 {
		t.Fatalf("batch find = %d %v", len(found), err)
	}

	seen := map[string]bool{}
	var after *mldomain.ListingList
	for page := 0; page < 5; page++ {
		in := mldomain.ListListingsInput{AccountID: f.account.ID, Limit: 2}
		if after != nil {
			in.After = after.Next
		}
		list, err := listings.ListByAccount(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range list.Items {
			if seen[l.ItemID] {
				t.Fatalf("%s repeated across pages", l.ItemID)
			}
			seen[l.ItemID] = true
		}
		if list.Next == nil {
			break
		}
		after = list
	}
	if len(seen) != 5 {
		t.Fatalf("paged %d listings", len(seen))
	}
	paused, _ := listings.ListByAccount(ctx, mldomain.ListListingsInput{AccountID: f.account.ID, Status: "paused", Limit: 10})
	searched, _ := listings.ListByAccount(ctx, mldomain.ListListingsInput{AccountID: f.account.ID, Search: "nova", Limit: 10})
	if len(paused.Items) != 1 || len(searched.Items) != 1 || searched.Items[0].ItemID != "MLB100" {
		t.Fatalf("paused=%d searched=%d", len(paused.Items), len(searched.Items))
	}
}

func TestAListingSyncIsClaimedOncePerWindow(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	listings := NewListingRepository(db)
	now := time.Now().UTC()

	const racers = 8
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := listings.ClaimSync(ctx, f.account.ID, now, now.Add(-6*time.Hour)); err == nil && ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d syncs claimed", wins.Load())
	}
	if ok, _ := listings.ClaimSync(ctx, f.account.ID, now.Add(time.Hour), now.Add(-5*time.Hour)); ok {
		t.Fatal("a sync inside the window was claimed again")
	}
	if ok, _ := listings.ClaimSync(ctx, f.account.ID, now.Add(7*time.Hour), now.Add(time.Hour)); !ok {
		t.Fatal("a sync after the window must be claimable")
	}
	if err := listings.ReleaseSync(ctx, f.account.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := listings.ClaimSync(ctx, f.account.ID, now.Add(8*time.Hour), now.Add(2*time.Hour)); !ok {
		t.Fatal("a released sync must be claimable")
	}
}

func TestThePurgeRepositoryEmptiesAnAccountInBatches(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	purge := NewPurgeRepository(db)
	questions := NewQuestionRepository(db)
	listings := NewListingRepository(db)
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		conv, _, err := f.conversations.FindOrCreateByPack(ctx, mldomain.FindOrCreateConversationInput{
			WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ContactID: f.contact.ID, PackKey: fmt.Sprintf("200000000000000%d", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			db.Model(&schema.MercadoLivreConversation{}).Where("id = ?", conv.ID).Update("deleted_at", now)
		}
		if _, err := questions.Upsert(ctx, openQuestion(f, fmt.Sprintf("1300000000%d", i), now)); err != nil {
			t.Fatal(err)
		}
	}
	synced := now
	if err := listings.UpsertMany(ctx, []*mldomain.Listing{
		{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: "MLB100", SyncedAt: &synced},
		{WorkspaceID: f.account.WorkspaceID, AccountID: f.account.ID, ItemID: "MLB101", SyncedAt: &synced},
	}); err != nil {
		t.Fatal(err)
	}
	_ = listings.SetThumbnail(ctx, f.account.ID, "MLB100", "k1.jpg", "src")
	if _, err := listings.ClaimSync(ctx, f.account.ID, now, now); err != nil {
		t.Fatal(err)
	}

	ids, err := purge.ConversationIDs(ctx, f.account.ID, 2)
	if err != nil || len(ids) != 2 {
		t.Fatalf("first batch = %v %v", ids, err)
	}
	every, _ := purge.ConversationIDs(ctx, f.account.ID, 10)
	for i, id := range every {
		summary := fmt.Sprintf(`{"items":[{"itemId":"MLB100"},{"itemId":"MLB200","thumbnailKey":"pack-%d.jpg"}]}`, i)
		if err := db.Unscoped().Model(&schema.MercadoLivreConversation{}).Where("id = ?", id).Update("pack_summary", summary).Error; err != nil {
			t.Fatal(err)
		}
	}
	packKeys, err := purge.PackThumbnailKeys(ctx, f.account.ID, every)
	sort.Strings(packKeys)
	if err != nil || strings.Join(packKeys, ",") != "pack-0.jpg,pack-1.jpg,pack-2.jpg" {
		t.Fatalf("pack thumbnails = %v %v, want every stored item thumbnail, soft deleted conversations included", packKeys, err)
	}
	if none, err := purge.PackThumbnailKeys(ctx, "00000000-0000-0000-0000-000000000000", every); err != nil || len(none) != 0 {
		t.Fatalf("another account's conversations = %v %v", none, err)
	}
	if err := purge.DeleteConversations(ctx, f.account.ID, ids); err != nil {
		t.Fatal(err)
	}
	ids, _ = purge.ConversationIDs(ctx, f.account.ID, 2)
	if len(ids) != 1 {
		t.Fatalf("soft deleted conversations must be purged too, left %v", ids)
	}
	_ = purge.DeleteConversations(ctx, f.account.ID, ids)
	if ids, _ = purge.ConversationIDs(ctx, f.account.ID, 2); len(ids) != 0 {
		t.Fatalf("left %v", ids)
	}

	if n, err := purge.DeleteQuestions(ctx, f.account.ID, 2); err != nil || n != 2 {
		t.Fatalf("question batch = %d %v", n, err)
	}
	if n, _ := purge.DeleteQuestions(ctx, f.account.ID, 2); n != 1 {
		t.Fatalf("question rest = %d", n)
	}

	thumbs, err := purge.ListingThumbnails(ctx, f.account.ID, 10)
	if err != nil || len(thumbs) != 2 {
		t.Fatalf("thumbnails = %v %v", thumbs, err)
	}
	keys := map[string]string{}
	var items []string
	for _, th := range thumbs {
		keys[th.ItemID] = th.Key
		items = append(items, th.ItemID)
	}
	if keys["MLB100"] != "k1.jpg" || keys["MLB101"] != "" {
		t.Fatalf("keys = %v", keys)
	}
	if err := purge.DeleteListings(ctx, f.account.ID, items); err != nil {
		t.Fatal(err)
	}
	if err := purge.DeleteListingSync(ctx, f.account.ID); err != nil {
		t.Fatal(err)
	}

	db.Model(&schema.MercadoLivreContact{}).Where("id = ?", f.contact.ID).Update("deleted_at", now)
	contacts, err := purge.ContactIDs(ctx, f.account.ID, 10)
	if err != nil || len(contacts) != 1 {
		t.Fatalf("contacts = %v %v", contacts, err)
	}
	if err := purge.DeleteContacts(ctx, f.account.ID, contacts); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&schema.MercadoLivreConversation{}, &schema.MercadoLivreQuestion{}, &schema.MercadoLivreListing{}, &schema.MercadoLivreListingSync{}, &schema.MercadoLivreContact{}} {
		var n int64
		db.Unscoped().Model(model).Where("account_id = ?", f.account.ID).Count(&n)
		if n != 0 {
			t.Errorf("%T kept %d rows", model, n)
		}
	}
}

func TestQuestionsAreFoundInBatchesAndForgottenOneByOne(t *testing.T) {
	db := catalogDB(t)
	ctx := context.Background()
	f := newFixture(t, db)
	questions := NewQuestionRepository(db)
	now := time.Now().UTC()
	for _, id := range []string{"13000000001", "13000000002"} {
		if _, err := questions.Upsert(ctx, openQuestion(f, id, now)); err != nil {
			t.Fatal(err)
		}
	}
	found, err := questions.FindByQuestionIDs(ctx, f.account.ID, []string{"13000000001", "13000000002", "13000000009", "13000000001"})
	if err != nil || len(found) != 2 {
		t.Fatalf("found = %d %v", len(found), err)
	}
	if other, _ := questions.FindByQuestionIDs(ctx, uuid.NewString(), []string{"13000000001"}); len(other) != 0 {
		t.Fatal("another account found the questions")
	}
	if err := questions.Forget(ctx, f.account.ID, "13000000001"); err != nil {
		t.Fatal(err)
	}
	if made, err := questions.Upsert(ctx, openQuestion(f, "13000000001", now)); err != nil || !made {
		t.Fatalf("a forgotten question is seen for the first time again: %v %v", made, err)
	}
}
