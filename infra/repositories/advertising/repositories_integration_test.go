package advertising_repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var channelTablesDDL = []string{
	`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL)`,
	`CREATE TABLE instagram_conversations (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE telegram_conversations (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE unofficial_whatsapp_conversations (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE facebook_conversations (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE webchat_widgets (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
	`CREATE TABLE webchat_conversations (id uuid PRIMARY KEY, widget_id uuid NOT NULL, workspace_id uuid)`,
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestAccountUpsertRefusesAMetaAccountOwnedByAnotherWorkspaceAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_account_test", &schema.AdAccount{})
	repo := NewAccountRepository(db)
	ctx := context.Background()
	wsA, wsB := uuid.NewString(), uuid.NewString()

	first := connectedAccount()
	first.WorkspaceID, first.GrantID = wsA, uuid.NewString()
	if err := repo.Upsert(ctx, first); err != nil {
		t.Fatal(err)
	}
	again := connectedAccount()
	again.WorkspaceID, again.GrantID, again.Name = wsA, uuid.NewString(), "Renomeada"
	if err := repo.Upsert(ctx, again); err != nil || again.ID != first.ID {
		t.Fatalf("refresh: id %q vs %q, %v", again.ID, first.ID, err)
	}
	stolen := connectedAccount()
	stolen.WorkspaceID, stolen.GrantID = wsB, uuid.NewString()
	if err := repo.Upsert(ctx, stolen); !errors.Is(err, advertising.ErrAccountLinkedElsewhere) {
		t.Fatalf("got %v", err)
	}
	kept, err := repo.FindByID(ctx, wsA, first.ID)
	if err != nil || kept.Name != "Renomeada" || kept.WorkspaceID != wsA {
		t.Fatalf("got %+v, %v", kept, err)
	}
}

func TestInsightDaysAndJobsRoundTripAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_insight_test",
		&schema.AdGrant{}, &schema.AdAccount{}, &schema.AdObject{}, &schema.AdInsightDaily{}, &schema.AdPublishJob{})
	ctx := context.Background()
	account := uuid.NewString()
	insights := NewInsightRepository(db)
	september := advertising.DateRange{Since: day("2026-09-01"), Until: day("2026-09-30")}
	if err := insights.ReplaceDays(ctx, account, september,
		[]advertising.DailyInsight{insightRow("ad-1", "2026-09-01"), insightRow("ad-2", "2026-09-01"), insightRow("ad-1", "2026-09-02")}); err != nil {
		t.Fatal(err)
	}
	if err := insights.ReplaceDays(ctx, account, advertising.DateRange{Since: day("2026-09-02"), Until: day("2026-09-02")}, nil); err != nil {
		t.Fatal(err)
	}
	rows, err := insights.Rows(ctx, account, september)
	if err != nil || len(rows) != 2 || rows[0].AdMetaID != "ad-1" || !rows[0].Day.Equal(day("2026-09-01")) ||
		rows[0].Actions[advertising.ActionConversationStarted] != 2 {
		t.Fatalf("got %+v, %v", rows, err)
	}
	if _, err := insights.AdDay(ctx, "ad-1", day("2026-09-02")); !errors.Is(err, advertising.ErrObjectNotFound) {
		t.Fatalf("got %v", err)
	}

	jobs := NewPublishJobRepository(db)
	job := queuedJob()
	job.WorkspaceID, job.AdAccountID, job.CreatedBy = uuid.NewString(), account, uuid.NewString()
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if won, err := jobs.Claim(ctx, job.ID, advertising.JobQueued, advertising.JobRunning); err != nil || !won {
		t.Fatalf("claim: %v, %v", won, err)
	}
	stale, err := jobs.ListStale(ctx, time.Now().Add(time.Minute), 10)
	if err != nil || len(stale) != 1 || stale[0].Status != advertising.JobRunning || stale[0].Draft.Campaign.Name != "Promo" ||
		len(stale[0].Draft.Ads) != 2 || stale[0].Draft.AdSet.Budget == nil || stale[0].Draft.AdSet.Budget.Amount != 2000 {
		t.Fatalf("got %+v, %v", stale, err)
	}
	if _, err := jobs.Find(ctx, uuid.NewString(), job.ID); !errors.Is(err, advertising.ErrJobNotFound) {
		t.Fatalf("job leaked across workspaces: %v", err)
	}
}

func TestReplaceLevelKeepsBudgetChangesAndMarksMissingObjectsRemovedAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_object_test", &schema.AdObject{})
	repo := NewObjectRepository(db)
	ctx := context.Background()
	ws, account := uuid.NewString(), uuid.NewString()
	object := func(metaID string) *advertising.Object {
		o := adSet(metaID)
		o.WorkspaceID, o.AdAccountID = ws, account
		return o
	}
	first := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := repo.ReplaceLevel(ctx, account, advertising.LevelAdSet, []*advertising.Object{object("s-1"), object("s-2")}, first); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.Find(ctx, ws, "s-1")
	if err != nil {
		t.Fatal(err)
	}
	changed.RecordBudgetChange(first.Add(time.Minute))
	if err := repo.Upsert(ctx, changed); err != nil {
		t.Fatal(err)
	}
	renamed := object("s-1")
	renamed.Name = "Novo nome"
	if err := repo.ReplaceLevel(ctx, account, advertising.LevelAdSet, []*advertising.Object{renamed}, first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	live, err := repo.List(ctx, advertising.ObjectQuery{WorkspaceID: ws, AdAccountID: account})
	if err != nil || len(live) != 1 {
		t.Fatalf("got %v, %v", live, err)
	}
	if live[0].Name != "Novo nome" || len(live[0].BudgetChanges) != 1 || !live[0].SyncedAt.Equal(first.Add(time.Hour)) {
		t.Fatalf("got %+v", live[0])
	}
	all, err := repo.List(ctx, advertising.ObjectQuery{WorkspaceID: ws, AdAccountID: account, IncludeRemoved: true})
	if err != nil || len(all) != 2 {
		t.Fatalf("got %v, %v", all, err)
	}

	if err := repo.ReplaceLevel(ctx, account, advertising.LevelAdSet, nil, first.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if none, err := repo.List(ctx, advertising.ObjectQuery{WorkspaceID: ws, AdAccountID: account}); err != nil || len(none) != 0 {
		t.Fatalf("got %v, %v", none, err)
	}
	if other, err := repo.List(ctx, advertising.ObjectQuery{WorkspaceID: uuid.NewString(), AdAccountID: account, IncludeRemoved: true}); err != nil || len(other) != 0 {
		t.Fatalf("leaked across workspaces: %v, %v", other, err)
	}
}

func TestAttributionCountsOnlyTheWorkspacesConversationsAndDealsAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_attribution_test",
		&schema.AdObject{}, &schema.ConversationAdOrigin{}, &schema.Opportunity{}, &schema.OpportunityConversation{})
	for _, ddl := range channelTablesDDL {
		mustExec(t, db, ddl)
	}
	ctx := context.Background()
	wsA, wsB := uuid.NewString(), uuid.NewString()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	inside := from.Add(48 * time.Hour)

	for _, ad := range []struct{ metaID, ws string }{{"ad-1", wsA}, {"ad-2", wsA}, {"ad-x", wsB}} {
		mustExec(t, db, `INSERT INTO ad_objects (meta_id, workspace_id, ad_account_id, level, synced_at) VALUES (?, ?, ?, 'ad', NOW())`,
			ad.metaID, ad.ws, uuid.NewString())
	}
	campaign := uuid.NewString()
	mustExec(t, db, `INSERT INTO whatsapp_campaigns (id, workspace_id) VALUES (?, ?)`, campaign, wsA)

	origin := func(table, entryType, ws, ad string, arrived time.Time) string {
		id := uuid.NewString()
		if table == "whatsapp_campaign_entries" {
			mustExec(t, db, `INSERT INTO whatsapp_campaign_entries (id, campaign_id) VALUES (?, ?)`, id, campaign)
		} else {
			mustExec(t, db, `INSERT INTO `+table+` (id, workspace_id) VALUES (?, ?)`, id, ws)
		}
		mustExec(t, db, `INSERT INTO conversation_ad_origins (entry_id, entry_type, ad_id, arrived_at) VALUES (?, ?, ?, ?)`,
			id, entryType, ad, arrived)
		return id
	}
	opportunity := func(ws, status, currency string, cents int64, entries ...[2]string) string {
		o := &schema.Opportunity{WorkspaceID: ws, PipelineID: uuid.NewString(), StageID: uuid.NewString(),
			Status: status, Currency: currency, ValueCents: cents}
		if err := db.Create(o).Error; err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			link := &schema.OpportunityConversation{OpportunityID: o.ID, EntryID: e[0], EntryType: e[1]}
			if err := db.Create(link).Error; err != nil {
				t.Fatal(err)
			}
		}
		return o.ID
	}

	whatsapp := origin("whatsapp_campaign_entries", "whatsapp", wsA, "ad-1", inside)
	instagram := origin("instagram_conversations", "instagram", wsA, "ad-1", inside)
	facebook := origin("facebook_conversations", "facebook", wsA, "ad-1", inside)
	foreign := origin("instagram_conversations", "instagram", wsB, "ad-1", inside)
	origin("whatsapp_campaign_entries", "whatsapp", wsA, "ad-1", from.Add(-time.Hour))
	telegram := origin("telegram_conversations", "telegram", wsA, "ad-2", inside)
	origin("telegram_conversations", "telegram", wsA, "ad-x", inside)

	opportunity(wsA, "won", "BRL", 1000, [2]string{whatsapp, "whatsapp"}, [2]string{instagram, "instagram"})
	opportunity(wsA, "open", "BRL", 9999, [2]string{whatsapp, "whatsapp"})
	opportunity(wsB, "won", "BRL", 7777, [2]string{foreign, "instagram"})
	deleted := opportunity(wsA, "won", "BRL", 5555, [2]string{facebook, "facebook"})
	if err := db.Delete(&schema.Opportunity{}, "id = ?", deleted).Error; err != nil {
		t.Fatal(err)
	}
	opportunity(wsA, "won", "BRL", 500, [2]string{telegram, "telegram"})
	opportunity(wsA, "won", "USD", 300, [2]string{telegram, "telegram"})

	repo := NewAttributionRepository(db)
	perAd := []advertising.AdGroup{{AdMetaID: "ad-1", Key: "ad-1"}, {AdMetaID: "ad-2", Key: "ad-2"}, {AdMetaID: "ad-x", Key: "ad-x"}}
	rows, err := repo.ByGroup(ctx, wsA, perAd, from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []advertising.Attribution{
		{Key: "ad-1", Conversations: 3, Leads: 2, WonDeals: 1, Revenue: 1000, RevenueCurrency: "BRL"},
		{Key: "ad-2", Conversations: 1, Leads: 1, WonDeals: 2, Revenue: 0, RevenueCurrency: "MIXED"},
	}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("got %+v", rows)
	}
	together := []advertising.AdGroup{{AdMetaID: "ad-1", Key: "all"}, {AdMetaID: "ad-2", Key: "all"}, {AdMetaID: "ad-x", Key: "all"}}
	total, err := repo.ByGroup(ctx, wsA, together, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(total) != 1 || total[0].Conversations != rows[0].Conversations+rows[1].Conversations {
		t.Fatalf("total %+v", total)
	}
	n, err := repo.Conversations(ctx, wsA, "ad-1", from, to)
	if err != nil || n != 3 {
		t.Fatalf("got %d, %v", n, err)
	}
	if n, err := repo.Conversations(ctx, wsB, "ad-1", from, to); err != nil || n != 0 {
		t.Fatalf("ad of another workspace counted: %d, %v", n, err)
	}
}

func TestNumberDirectoryReturnsTheRecordedPageLinksAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_number_links_test", &schema.AdPageWhatsAppLink{})
	mustExec(t, db, `CREATE TABLE whatsapp_business_phone_numbers (id uuid PRIMARY KEY, owner_workspace_id uuid, verified_name text, display_phone_number text, business_portfolio_id text, deleted_at timestamptz)`)
	mustExec(t, db, `CREATE TABLE unofficial_whatsapp_instances (id uuid PRIMARY KEY, workspace_id uuid, display_name text, profile_name text, phone_number text, deleted_at timestamptz)`)
	ctx := context.Background()
	wsA, wsB := uuid.NewString(), uuid.NewString()
	mustExec(t, db, `INSERT INTO whatsapp_business_phone_numbers VALUES (?, ?, 'Vozkoia Dev', '+55 11 96546-7700', 'biz-1', NULL)`, uuid.NewString(), wsA)
	mustExec(t, db, `INSERT INTO unofficial_whatsapp_instances VALUES (?, ?, 'Vendas', '', '5511977776666', NULL)`, uuid.NewString(), wsA)
	directory := NewNumberDirectory(db)
	for _, page := range []string{"page-2", "page-1"} {
		if err := directory.RecordPageLink(ctx, wsA, page, "+55 (11) 96546-7700"); err != nil {
			t.Fatal(err)
		}
	}
	if err := directory.RecordPageLink(ctx, wsA, "page-1", "5511965467700"); err != nil {
		t.Fatal(err)
	}
	if err := directory.RecordPageLink(ctx, wsB, "page-9", "5511977776666"); err != nil {
		t.Fatal(err)
	}
	numbers, err := directory.List(ctx, wsA)
	if err != nil {
		t.Fatal(err)
	}
	if len(numbers) != 2 || numbers[0].PortfolioID != "biz-1" || strings.Join(numbers[0].LinkedPageIDs, ",") != "page-1,page-2" || len(numbers[1].LinkedPageIDs) != 0 {
		t.Fatalf("numbers %+v", numbers)
	}
}

func TestACampaignScopeIncludesTheCampaignAndItsAdSetScopeTheAdSetAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "ads_object_scope_test", &schema.AdObject{})
	repo := NewObjectRepository(db)
	ctx := context.Background()
	ws, account := uuid.NewString(), uuid.NewString()
	objects := []*advertising.Object{
		{MetaID: "c-1", Level: advertising.LevelCampaign, Name: "Campanha"},
		{MetaID: "c-2", Level: advertising.LevelCampaign, Name: "Outra"},
		{MetaID: "s-1", Level: advertising.LevelAdSet, CampaignMetaID: "c-1", Name: "Conjunto"},
		{MetaID: "a-1", Level: advertising.LevelAd, CampaignMetaID: "c-1", AdSetMetaID: "s-1", Name: "Anúncio"},
	}
	for _, o := range objects {
		o.WorkspaceID, o.AdAccountID = ws, account
		if err := repo.Upsert(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	scoped := func(level advertising.Level, q advertising.ObjectQuery) []string {
		q.WorkspaceID, q.AdAccountID, q.Level = ws, account, level
		found, err := repo.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(found))
		for _, o := range found {
			ids = append(ids, o.MetaID)
		}
		return ids
	}
	if got := scoped(advertising.LevelCampaign, advertising.ObjectQuery{CampaignIDs: []string{"c-1"}}); strings.Join(got, ",") != "c-1" {
		t.Fatalf("campaign scope listed %v", got)
	}
	if got := scoped(advertising.LevelAdSet, advertising.ObjectQuery{CampaignIDs: []string{"c-1"}}); strings.Join(got, ",") != "s-1" {
		t.Fatalf("ad sets of the campaign %v", got)
	}
	if got := scoped(advertising.LevelAdSet, advertising.ObjectQuery{AdSetIDs: []string{"s-1"}}); strings.Join(got, ",") != "s-1" {
		t.Fatalf("ad set scope listed %v", got)
	}
	if got := scoped(advertising.LevelAd, advertising.ObjectQuery{CampaignIDs: []string{"c-1"}, AdSetIDs: []string{"s-1"}}); strings.Join(got, ",") != "a-1" {
		t.Fatalf("ads of the ad set %v", got)
	}
	if got := scoped(advertising.LevelCampaign, advertising.ObjectQuery{CampaignIDs: []string{"c-2"}}); strings.Join(got, ",") != "c-2" {
		t.Fatalf("other campaign %v", got)
	}
}
