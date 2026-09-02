package comment_analysis_repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

// Integration tests against a real Postgres. Opt-in: set VOZKO_TEST_DB=1 and
// the same DB_* variables the application reads. Each run works in its own
// throwaway schema and drops it, so the development database is untouched.
//
// These exist because the claims sqlmock cannot verify are the ones the
// engine rests on: that concurrent ClaimByIDs calls receive DISJOINT rows,
// that a duplicate insert race yields exactly one row, and that every
// COUNT(*) FILTER column in GetStats counts what its name says.

func baseDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
}

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(baseDSN()), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "ca_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	// search_path travels in the DSN, so every pooled connection the tests
	// open lands in the throwaway schema.
	db, err := gorm.Open(postgres.Open(baseDSN()+" search_path="+schemaName), silent)
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

	if err := db.AutoMigrate(
		&schema.CommentAnalysis{}, &schema.CommentAnalysisSettings{}, &schema.CommentAnalysisAuthor{},
		&schema.CommentAnalysisRollup{}, &schema.CommentAnalysisBatch{}, &schema.CommentAnalysisBackfill{},
		&schema.CommentAnalysisContainerSettings{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX ux_ca_source_comment ON comment_analyses (source, source_comment_id)`,
		`CREATE UNIQUE INDEX ux_ca_author ON comment_analysis_authors (source, account_id, author_external_id)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index: %v", err)
		}
	}
	return db
}

func seedPending(t *testing.T, repo ca.Repository, n int, author string) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		a, err := ca.NewPending(ca.NewInput{
			WorkspaceID: "11111111-1111-1111-1111-111111111111", Container: integrationRef(),
			SourceCommentID: fmt.Sprintf("c-%s-%d", author, i), AuthorExternalID: author, AuthorHandle: author,
			Text: fmt.Sprintf("comentário %d", i), Now: now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		a.ID = uuid.New().String()
		if _, err := repo.Insert(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	return ids
}

func integrationRef() ca.ContainerRef {
	return ca.ContainerRef{Source: ca.SourceInstagram, AccountID: "22222222-2222-2222-2222-222222222222", ContainerID: "media-1"}
}

// THE concurrency test the plan asks for: N replicas that all listed the
// same pending rows and all try to claim them. Every row is claimed exactly
// once, and the losers see absence, not errors.
func TestIntegration_ConcurrentClaimByIDsIsDisjoint(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	seeded := seedPending(t, repo, 40, "u-1")

	listed, err := repo.ListPending(context.Background(), integrationRef(), 100)
	if err != nil || len(listed) != 40 {
		t.Fatalf("ListPending: %v %d", err, len(listed))
	}
	// Oldest first: the order the batches are planned in.
	if listed[0].SourceCommentID != "c-u-1-0" || listed[39].SourceCommentID != "c-u-1-39" {
		t.Fatalf("ListPending order: %s .. %s", listed[0].SourceCommentID, listed[39].SourceCommentID)
	}

	const claimers = 6
	var wg sync.WaitGroup
	results := make([][]string, claimers)
	for i := 0; i < claimers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each replica tries for a different overlapping window of the
			// same rows, the way two ticks that planned from the same list
			// would.
			from, to := (i*10)%30, (i*10)%30+20
			ids := make([]string, 0, 20)
			for _, r := range listed[from:to] {
				ids = append(ids, r.ID)
			}
			claimed, err := repo.ClaimByIDs(context.Background(), ids, now)
			if err != nil {
				t.Errorf("claimer %d: %v", i, err)
				return
			}
			results[i] = claimed
		}(i)
	}
	wg.Wait()

	seen := map[string]int{}
	for _, ids := range results {
		for _, id := range ids {
			seen[id]++
		}
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("row %s was claimed %d times", id, n)
		}
	}
	// Windows covered rows 0..39 between them; every one is claimed once.
	if len(seen) != len(seeded) {
		t.Fatalf("claimed %d distinct rows, want all %d", len(seen), len(seeded))
	}
	left, err := repo.ListPending(context.Background(), integrationRef(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("%d rows still pending after the race", len(left))
	}
	var attempts []int
	if err := db.Model(&schema.CommentAnalysis{}).Pluck("attempts", &attempts).Error; err != nil {
		t.Fatal(err)
	}
	for _, a := range attempts {
		if a != 1 {
			t.Fatalf("a row carries %d attempts after one claim", a)
		}
	}
}

// A redelivered webhook races the original: exactly one row exists after.
func TestIntegration_DuplicateInsertRaceYieldsOneRow(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	const writers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	insertedCount := 0
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, _ := ca.NewPending(ca.NewInput{
				WorkspaceID: "11111111-1111-1111-1111-111111111111", Container: integrationRef(),
				SourceCommentID: "dup-1", AuthorExternalID: "u-1", Text: "oi", Now: now,
			})
			a.ID = uuid.New().String()
			inserted, err := repo.Insert(context.Background(), a)
			if err != nil {
				t.Errorf("insert: %v", err)
				return
			}
			if inserted {
				mu.Lock()
				insertedCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if insertedCount != 1 {
		t.Fatalf("%d writers reported inserted, want exactly 1", insertedCount)
	}
	var n int64
	if err := db.Model(&schema.CommentAnalysis{}).Where("source_comment_id = ?", "dup-1").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d rows for one comment", n)
	}
}

// Every FILTER column, against a fixture whose numbers are all different so
// a swapped column cannot pass by coincidence.
func TestIntegration_GetStatsCountsEveryColumn(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"

	lvl := func(sent shared.Sentiment, st ca.Stance, in ca.Intent, topic string, spam bool, tox shared.QualityLevel) ca.Classification {
		return ca.Classification{Sentiment: sent, Stance: st, Intent: in, TopicKey: topic, IsSpam: spam, Language: "pt",
			Toxicity: tox, PersonalAttack: shared.QualityLevelNone, LegalRisk: shared.QualityLevelNone}
	}
	topics := ca.TopicSet{{Key: "saude", Label: "Saúde"}, {Key: "asfalto", Label: "Asfalto"}}.Normalize()
	analyzed := []ca.Classification{
		lvl(shared.SentimentPositive, ca.StanceSupporter, ca.IntentPraise, "saude", false, shared.QualityLevelNone),
		lvl(shared.SentimentPositive, ca.StanceSupporter, ca.IntentPraise, "saude", false, shared.QualityLevelNone),
		lvl(shared.SentimentPositive, ca.StanceSupporter, ca.IntentQuestion, "asfalto", false, shared.QualityLevelNone),
		lvl(shared.SentimentNeutral, ca.StanceNeutral, ca.IntentQuestion, "asfalto", false, shared.QualityLevelNone),
		lvl(shared.SentimentNeutral, ca.StanceNeutral, ca.IntentOther, "other", false, shared.QualityLevelNone),
		lvl(shared.SentimentNegative, ca.StanceCritic, ca.IntentComplaint, "saude", false, shared.QualityLevelLow),
		lvl(shared.SentimentNegative, ca.StanceCritic, ca.IntentComplaint, "saude", false, shared.QualityLevelLow),
		lvl(shared.SentimentNegative, ca.StanceCritic, ca.IntentSupportRequest, "asfalto", false, shared.QualityLevelLow),
		lvl(shared.SentimentNegative, ca.StanceHostile, ca.IntentOther, "other", false, shared.QualityLevelHigh),     // 45
		lvl(shared.SentimentNegative, ca.StanceHostile, ca.IntentSalesLead, "other", false, shared.QualityLevelHigh), // 45
		lvl(shared.SentimentNegative, ca.StanceHostile, ca.IntentSpam, "other", true, shared.QualityLevelHigh),       // 45
	}
	// Push three of the hostile ones over the high threshold with a personal attack.
	analyzed[8].PersonalAttack = shared.QualityLevelHigh  // 80
	analyzed[9].PersonalAttack = shared.QualityLevelHigh  // 80
	analyzed[10].PersonalAttack = shared.QualityLevelHigh // 80

	i := 0
	insert := func(author string, apply *ca.Classification, status ca.Status) {
		a, err := ca.NewPending(ca.NewInput{WorkspaceID: ws, Container: integrationRef(),
			SourceCommentID: fmt.Sprintf("s-%d", i), AuthorExternalID: author, Text: "x", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		i++
		a.ID = uuid.New().String()
		if _, err := repo.Insert(ctx, a); err != nil {
			t.Fatal(err)
		}
		switch status {
		case ca.StatusAnalyzed:
			_ = a.Claim(now)
			c := *apply
			if err := c.Validate(topics); err != nil {
				t.Fatal(err)
			}
			if err := a.Apply(c, ca.ActionPolicy{}, ca.Provenance{BatchID: uuid.New().String(), Model: "m"}, now); err != nil {
				t.Fatal(err)
			}
		case ca.StatusFailed:
			_ = a.Claim(now)
			_ = a.Fail("boom", now)
		case ca.StatusInFlight:
			_ = a.Claim(now)
		}
		if status != ca.StatusPending {
			if err := repo.Save(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	authors := []string{"a1", "a1", "a1", "a2", "a2", "a3", "a3", "a4", "a5", "a5", "a5"}
	for k := range analyzed {
		insert(authors[k], &analyzed[k], ca.StatusAnalyzed)
	}
	insert("a6", nil, ca.StatusPending)
	insert("a6", nil, ca.StatusPending)
	insert("a7", nil, ca.StatusFailed)
	insert("a7", nil, ca.StatusInFlight)
	// A soft-deleted row must not count anywhere in the live stats.
	insert("a8", &analyzed[0], ca.StatusAnalyzed)
	if err := repo.SoftDeleteBySourceComment(ctx, ca.SourceInstagram, fmt.Sprintf("s-%d", i-1), now); err != nil {
		t.Fatal(err)
	}

	stats, err := repo.GetStats(ctx, ca.ListInput{WorkspaceID: ws, AccountID: integrationRef().AccountID})
	if err != nil {
		t.Fatal(err)
	}
	c := stats.Counters
	checks := map[string][2]int{
		"Total":                {c.Total, 15},
		"Analyzed":             {c.Analyzed, 11},
		"Pending":              {c.Pending, 2},
		"Failed":               {c.Failed, 1},
		"InFlight":             {c.InFlight, 1},
		"Skipped":              {c.Skipped, 0},
		"SentimentPositive":    {c.SentimentPositive, 3},
		"SentimentNeutral":     {c.SentimentNeutral, 2},
		"SentimentNegative":    {c.SentimentNegative, 6},
		"StanceSupporter":      {c.StanceSupporter, 3},
		"StanceNeutral":        {c.StanceNeutral, 2},
		"StanceCritic":         {c.StanceCritic, 3},
		"StanceHostile":        {c.StanceHostile, 3},
		"IntentPraise":         {c.IntentPraise, 2},
		"IntentQuestion":       {c.IntentQuestion, 2},
		"IntentComplaint":      {c.IntentComplaint, 2},
		"IntentSupportRequest": {c.IntentSupportRequest, 1},
		"IntentSpam":           {c.IntentSpam, 1},
		"IntentSalesLead":      {c.IntentSalesLead, 1},
		"IntentOther":          {c.IntentOther, 2},
		"SpamCount":            {c.SpamCount, 1},
		"SeverityMax":          {c.SeverityMax, 80},
		"SeverityHighCount":    {c.SeverityHighCount, 3},
		"RequiresActionCount":  {c.RequiresActionCount, 8}, // 2 question + 2 complaint + 1 support + 3 severity≥60
		"DistinctAuthors":      {c.DistinctAuthors, 7},     // a1..a7; a8 is deleted
	}
	for name, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %d, want %d", name, v[0], v[1])
		}
	}
	// (3×15 + 3×80) / 11 = 285/11 ≈ 25.9 (low toxicity alone scores 15)
	if c.SeverityAvg < 25.8 || c.SeverityAvg > 26.0 {
		t.Errorf("SeverityAvg = %v, want ≈25.9", c.SeverityAvg)
	}
	topicCounts := map[string]int{}
	for _, tp := range stats.Topics {
		topicCounts[tp.TopicKey] = tp.Count
	}
	if topicCounts["saude"] != 4 || topicCounts["asfalto"] != 3 || topicCounts["other"] != 4 {
		t.Errorf("topics = %+v", topicCounts)
	}

	// Filters narrow the same slice: hostile only.
	hostile, err := repo.GetStats(ctx, ca.ListInput{WorkspaceID: ws, Stance: ca.StanceHostile})
	if err != nil {
		t.Fatal(err)
	}
	if hostile.Total != 3 || hostile.SeverityHighCount != 3 {
		t.Errorf("hostile slice = %+v", hostile.Counters)
	}

	// Another workspace sees nothing: scoping is in the SQL, not the caller.
	other, err := repo.GetStats(ctx, ca.ListInput{WorkspaceID: "33333333-3333-3333-3333-333333333333"})
	if err != nil {
		t.Fatal(err)
	}
	if other.Total != 0 {
		t.Errorf("another workspace saw %d rows", other.Total)
	}

	// The author projection: a5 wrote all three hostile, high-severity
	// comments and is the one author the dashboard must name.
	agg, err := repo.AggregateAuthors(ctx, ca.SourceInstagram, integrationRef().AccountID, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	byAuthor := map[string]*ca.AuthorStats{}
	for _, a := range agg {
		a.Derive()
		byAuthor[a.AuthorExternalID] = a
	}
	if a5 := byAuthor["a5"]; a5 == nil || a5.StanceHostile != 3 || a5.SeverityHighCount != 3 || !a5.IsFlagged || a5.DerivedStance != ca.StanceHostile {
		t.Errorf("a5 = %+v", byAuthor["a5"])
	}
	if a1 := byAuthor["a1"]; a1 == nil || a1.DerivedStance != ca.StanceSupporter || a1.IsFlagged {
		t.Errorf("a1 = %+v", byAuthor["a1"])
	}
	if a6 := byAuthor["a6"]; a6 == nil || a6.Total != 2 || a6.Analyzed != 0 {
		t.Errorf("a6 (pending only) = %+v", byAuthor["a6"])
	}
	if _, ok := byAuthor["a8"]; ok {
		t.Error("a deleted author's rows must not build a projection")
	}

	// Rollups for the day include the analysed rows, deleted or not.
	rollups, err := repo.AggregateRollups(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	var account *ca.Rollup
	topicScopes := 0
	for _, r := range rollups {
		switch r.Scope {
		case ca.ScopeAccount:
			account = r
		case ca.ScopeTopic:
			topicScopes++
		}
	}
	if account == nil || account.Analyzed != 12 { // 11 live + 1 deleted
		t.Errorf("account rollup = %+v", account)
	}
	if topicScopes != 3 {
		t.Errorf("expected 3 topic rollups, got %d", topicScopes)
	}
	days, err := repo.DaysAnalyzedSince(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || !days[0].Equal(ca.BucketDate(now)) {
		t.Errorf("days = %v", days)
	}
}

func TestIntegration_ListPaginatesAndOrders(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	seedPending(t, repo, 25, "u-1")

	page, err := repo.List(context.Background(), ca.ListInput{
		WorkspaceID: "11111111-1111-1111-1111-111111111111",
		Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: 2, PageSize: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 25 || page.TotalPages != 3 || len(page.Items) != 10 {
		t.Fatalf("page = %d/%d items=%d", page.TotalItems, page.TotalPages, len(page.Items))
	}
	// Newest first by commented_at.
	ids := make([]string, len(page.Items))
	for i, it := range page.Items {
		ids[i] = it.SourceCommentID
	}
	if !sort.SliceIsSorted(page.Items, func(i, j int) bool {
		return page.Items[i].CommentedAt.After(page.Items[j].CommentedAt)
	}) {
		t.Fatalf("not newest-first: %v", ids)
	}
}

func TestIntegration_StaleInFlightAndPurge(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	ids := seedPending(t, repo, 3, "u-1")

	claimed, err := repo.ClaimByIDs(ctx, ids[:2], now.Add(-time.Hour))
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim: %v %d", err, len(claimed))
	}
	stale, err := repo.ListStaleInFlight(ctx, now.Add(-10*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 2 {
		t.Fatalf("stale = %d, want 2", len(stale))
	}
	pending, err := repo.CountPendingBySource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pending[ca.SourceInstagram] != 1 {
		t.Fatalf("pending gauge = %v", pending)
	}
	// Purge never touches in_flight rows: a slow batch must not lose its
	// rows under it.
	n, err := repo.PurgeBefore(ctx, now.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("purged %d, want the 1 pending row only", n)
	}
}

func TestIntegration_SettingsAuthorsRollupsRoundTrip(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"

	settings := NewSettingsRepository(db)
	if _, err := settings.Find(ctx, ca.SourceInstagram, integrationRef().AccountID); err != ca.ErrNotFound {
		t.Fatalf("unconfigured account should be ErrNotFound, got %v", err)
	}
	s := ca.NewSettings(ws, ca.SourceInstagram, integrationRef().AccountID, ca.VerticalGov)
	s.Enabled = true
	s.UpdatedAt = now
	if err := settings.Save(ctx, &s); err != nil {
		t.Fatal(err)
	}
	s.DailyCap = 500
	if err := settings.Save(ctx, &s); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Find(ctx, ca.SourceInstagram, integrationRef().AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.DailyCap != 500 || !got.Topics.Has("saude") || got.Vertical != ca.VerticalGov {
		t.Fatalf("settings round trip: %+v", got)
	}
	enabled, err := settings.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 {
		t.Fatalf("ListEnabled: %v %d", err, len(enabled))
	}

	authors := NewAuthorRepository(db)
	a := &ca.AuthorStats{WorkspaceID: ws, Source: ca.SourceInstagram, AccountID: integrationRef().AccountID,
		AuthorExternalID: "u-9", AuthorHandle: "nine", FirstSeenAt: now, LastSeenAt: now,
		Counters: ca.Counters{Total: 3, Analyzed: 3, StanceHostile: 3, SeverityHighCount: 3, SeverityMax: 90}, UpdatedAt: now}
	a.Derive()
	if err := authors.UpsertMany(ctx, []*ca.AuthorStats{a}); err != nil {
		t.Fatal(err)
	}
	page, err := authors.List(ctx, ca.AuthorsInput{WorkspaceID: ws, FlaggedOnly: true})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List flagged: %v %d", err, len(page.Items))
	}
	id := page.Items[0].ID
	if err := authors.SetModerationState(ctx, ws, id, ca.ModerationBlocked, now); err != nil {
		t.Fatal(err)
	}
	// A rebuild must not undo the operator's decision.
	a.AuthorHandle = "nine-renamed"
	if err := authors.UpsertMany(ctx, []*ca.AuthorStats{a}); err != nil {
		t.Fatal(err)
	}
	after, err := authors.FindByID(ctx, ws, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.ModerationState != ca.ModerationBlocked || after.AuthorHandle != "nine-renamed" {
		t.Fatalf("after rebuild: %+v", after)
	}
	if err := authors.SetModerationState(ctx, "33333333-3333-3333-3333-333333333333", id, ca.ModerationNone, now); err != ca.ErrNotFound {
		t.Fatalf("another workspace must not reach the row, got %v", err)
	}

	rollups := NewRollupRepository(db)
	r := &ca.Rollup{WorkspaceID: ws, Source: ca.SourceInstagram, AccountID: integrationRef().AccountID,
		Scope: ca.ScopeAccount, ScopeID: integrationRef().AccountID, BucketDate: ca.BucketDate(now),
		Counters: ca.Counters{Analyzed: 30, StanceSupporter: 30}, ComputedAt: now}
	r.Finalize()
	if err := rollups.UpsertMany(ctx, []*ca.Rollup{r}); err != nil {
		t.Fatal(err)
	}
	r.Counters.StanceSupporter = 20
	r.Counters.StanceHostile = 10
	r.Finalize()
	if err := rollups.UpsertMany(ctx, []*ca.Rollup{r}); err != nil {
		t.Fatal(err)
	}
	series, err := rollups.ListSeries(ctx, ca.TrendInput{WorkspaceID: ws, Scope: ca.ScopeAccount,
		ScopeID: integrationRef().AccountID, From: now.Add(-7 * 24 * time.Hour), To: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].StanceHostile != 10 || series[0].AcceptanceScore != r.AcceptanceScore {
		t.Fatalf("series = %+v", series)
	}

	batches := NewBatchRepository(db)
	for i := 0; i < 2; i++ {
		if err := batches.Create(ctx, &ca.Batch{ID: uuid.New().String(), WorkspaceID: ws, Source: ca.SourceInstagram,
			AccountID: integrationRef().AccountID, ContainerID: "media-1", Model: "m", ItemCount: 20, PromptTokens: 1000,
			CompletionTokens: 300, PriceMicros: 50, Outcome: ca.OutcomeOK, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	totals, err := batches.Totals(ctx, ws, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if totals.Batches != 2 || totals.Items != 40 || totals.PriceMicros != 100 {
		t.Fatalf("totals = %+v", totals)
	}

	backfills := NewBackfillRepository(db)
	b := &ca.Backfill{WorkspaceID: ws, Source: ca.SourceInstagram, AccountID: integrationRef().AccountID,
		Status: ca.BackfillPending, EstimatedComments: 100, RequestedByUserID: uuid.New().String(), CreatedAt: now, UpdatedAt: now}
	if err := backfills.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	active, err := backfills.FindActive(ctx, ca.SourceInstagram, integrationRef().AccountID, "")
	if err != nil || active.ID != b.ID {
		t.Fatalf("FindActive: %v %+v", err, active)
	}
	claimed, err := backfills.ClaimNextPending(ctx, now)
	if err != nil || claimed == nil || claimed.Status != ca.BackfillRunning {
		t.Fatalf("ClaimNextPending: %v %+v", err, claimed)
	}
	if again, _ := backfills.ClaimNextPending(ctx, now); again != nil {
		t.Fatal("a running backfill must not be claimed twice")
	}
	claimed.Advance("cur-2", 25, 20, now)
	if err := backfills.Save(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	found, err := backfills.FindByID(ctx, ws, claimed.ID)
	if err != nil || found.Cursor != "cur-2" || found.Fetched != 25 {
		t.Fatalf("FindByID: %v %+v", err, found)
	}
}

// Per-post overrides: the upsert keeps one row per post, nulls mean
// "inherit" all the way through Postgres and back, ListByWorkspace never
// crosses workspaces, and a delete is idempotent.
func TestIntegration_ContainerOverrideRoundTrip(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"
	other := "33333333-3333-3333-3333-333333333333"
	ref := integrationRef()

	settings := NewSettingsRepository(db)
	s := ca.NewSettings(ws, ca.SourceInstagram, ref.AccountID, ca.VerticalGov)
	s.Enabled = true
	s.Instructions = "Conta da prefeitura"
	s.UpdatedAt = now
	if err := settings.Save(ctx, &s); err != nil {
		t.Fatal(err)
	}
	got, err := settings.Find(ctx, ca.SourceInstagram, ref.AccountID)
	if err != nil || got.Instructions != "Conta da prefeitura" {
		t.Fatalf("instructions must round-trip on the account: %+v %v", got, err)
	}

	if _, err := settings.FindOverride(ctx, ref); err != ca.ErrNotFound {
		t.Fatalf("no override yet: want ErrNotFound, got %v", err)
	}

	off := false
	threshold := 80
	instr := "Post sobre a obra da Rua A"
	o := &ca.ContainerOverride{WorkspaceID: ws, Source: ref.Source, AccountID: ref.AccountID, ContainerID: ref.ContainerID,
		Enabled: &off, SeverityThreshold: &threshold, Instructions: &instr, UpdatedAt: now}
	if err := settings.SaveOverride(ctx, o); err != nil {
		t.Fatal(err)
	}
	// Second save on the same post updates in place: Enabled cleared back to
	// "inherit", topics set.
	o.Enabled = nil
	o.Topics = &ca.TopicSet{{Key: "obra", Label: "Obra"}}
	if err := settings.SaveOverride(ctx, o); err != nil {
		t.Fatal(err)
	}
	back, err := settings.FindOverride(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if back.Enabled != nil || back.Model != nil || back.SeverityThreshold == nil || *back.SeverityThreshold != 80 ||
		back.Instructions == nil || *back.Instructions != instr || back.Topics == nil || !back.Topics.Has("obra") {
		t.Fatalf("override round trip: %+v", back)
	}
	if back.WorkspaceID != ws {
		t.Fatalf("workspace lost: %q", back.WorkspaceID)
	}

	// ListOverrides is keyed by account (ownership is checked by the use
	// case through the verifier); another account of the same source sees
	// nothing.
	list, err := settings.ListOverrides(ctx, ca.SourceInstagram, ref.AccountID)
	if err != nil || len(list) != 1 || list[0].ContainerID != ref.ContainerID || list[0].WorkspaceID != ws {
		t.Fatalf("ListOverrides: %v %+v", err, list)
	}
	if list, _ := settings.ListOverrides(ctx, ca.SourceInstagram, "44444444-4444-4444-4444-444444444444"); len(list) != 0 {
		t.Fatal("another account must not see the override")
	}

	accounts, err := settings.ListByWorkspace(ctx, ws)
	if err != nil || len(accounts) != 1 || accounts[0].AccountID != ref.AccountID {
		t.Fatalf("ListByWorkspace: %v %+v", err, accounts)
	}
	if accounts, _ := settings.ListByWorkspace(ctx, other); len(accounts) != 0 {
		t.Fatal("another workspace must list no accounts")
	}

	if err := settings.DeleteOverride(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.FindOverride(ctx, ref); err != ca.ErrNotFound {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
	if err := settings.DeleteOverride(ctx, ref); err != nil {
		t.Fatalf("deleting a missing override must be a no-op, got %v", err)
	}
}
