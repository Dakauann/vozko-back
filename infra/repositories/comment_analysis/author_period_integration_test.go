package comment_analysis_repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// Integration tests for the WINDOWED author ranking. They share the harness in
// integration_test.go and, like it, are opt-in behind VOZKO_TEST_DB=1.
//
// These are the tests that justify the second code path existing at all: that
// a window changes the answer, and that a window wide enough to hold
// everything does NOT.

// seedAuthorComments writes n analysed comments by one person at a given time,
// so a windowed ranking has something real to regroup.
func seedAuthorComments(t *testing.T, repo ca.Repository, ws, author string, n int, st ca.Stance, harm shared.QualityLevel, at time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		seedAnalyzedIn(t, repo, ws, "media-1", author,
			fmt.Sprintf("%s-%s-%d", author, at.Format("0102"), i),
			classification(st, sentimentFor(st), harm),
			at.Add(time.Duration(i)*time.Second))
	}
}

func sentimentFor(st ca.Stance) shared.Sentiment {
	switch st {
	case ca.StanceSupporter:
		return shared.SentimentPositive
	case ca.StanceHostile, ca.StanceCritic:
		return shared.SentimentNegative
	}
	return shared.SentimentNeutral
}

func periodHandles(t *testing.T, repo ca.AuthorRepository, in ca.AuthorsInput) ([]string, int64) {
	t.Helper()
	in.Normalize()
	if err := in.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	page, err := repo.List(context.Background(), in)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(page.Items))
	for _, a := range page.Items {
		out = append(out, a.AuthorHandle)
	}
	return out, page.TotalItems
}

// THE test the windowed ranking exists for: the answer must change with the
// window. Someone hostile last month and quiet since must not head the ranking
// for this week.
func TestIntegration_AuthorRankingRespectsTheWindow(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	lastMonth := now.Add(-40 * 24 * time.Hour)
	thisWeek := now.Add(-2 * 24 * time.Hour)

	// Loud last month, silent since.
	seedAuthorComments(t, repo, ws, "ext-old", 6, ca.StanceHostile, shared.QualityLevelHigh, lastMonth)
	// Quiet last month, hostile this week.
	seedAuthorComments(t, repo, ws, "ext-new", 5, ca.StanceHostile, shared.QualityLevelHigh, thisWeek)
	// Supportive, so the other end of the ranking has an occupant.
	seedAuthorComments(t, repo, ws, "ext-fan", 4, ca.StanceSupporter, shared.QualityLevelNone, thisWeek)

	weekFrom := now.Add(-7 * 24 * time.Hour)
	week := ca.AuthorsInput{
		WorkspaceID: ws, AccountID: integrationRef().AccountID,
		From: &weekFrom, To: &now,
		Sort: ca.DefaultAuthorSort,
	}
	got, total := periodHandles(t, authors, week)
	if total != 2 {
		t.Fatalf("total = %d, want the 2 people who commented this week", total)
	}
	if len(got) != 2 || got[0] != "ext-new" {
		t.Fatalf("this week = %v, want the recent hostile first", got)
	}
	for _, h := range got {
		if h == "ext-old" {
			t.Fatal("someone silent all week must not appear in this week's ranking")
		}
	}

	// Widen the window and last month's author comes back, ahead of the newer
	// one because they said more.
	monthFrom := now.Add(-60 * 24 * time.Hour)
	wide := week
	wide.From = &monthFrom
	got, total = periodHandles(t, authors, wide)
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if got[0] != "ext-old" {
		t.Fatalf("wide window = %v, want the loudest hostile first", got)
	}
	if got[len(got)-1] != "ext-fan" {
		t.Fatalf("wide window = %v, want the supporter last on an ascending reputation sort", got)
	}
}

// The two code paths must not disagree. A window wide enough to contain
// everything has to rank people exactly as the lifetime projection does,
// because it is the same question asked twice.
func TestIntegration_WideWindowMatchesTheLifetimeRanking(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	at := now.Add(-3 * 24 * time.Hour)
	seedAuthorComments(t, repo, ws, "ext-a", 5, ca.StanceHostile, shared.QualityLevelHigh, at)
	seedAuthorComments(t, repo, ws, "ext-b", 3, ca.StanceCritic, shared.QualityLevelNone, at)
	seedAuthorComments(t, repo, ws, "ext-c", 7, ca.StanceSupporter, shared.QualityLevelNone, at)
	seedAuthorComments(t, repo, ws, "ext-d", 2, ca.StanceNeutral, shared.QualityLevelNone, at)

	// Build the lifetime projection from the same comments, the way the rollup
	// does, so both paths start from one set of facts.
	rebuilt, err := repo.AggregateAuthors(context.Background(), ca.SourceInstagram, integrationRef().AccountID, now.Add(-365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rebuilt {
		a.Derive()
	}
	if err := authors.UpsertMany(context.Background(), rebuilt); err != nil {
		t.Fatal(err)
	}

	wideFrom := now.Add(-300 * 24 * time.Hour)
	for _, key := range ca.AllAuthorSortKeys() {
		for _, asc := range []bool{true, false} {
			sortBy := ca.Sort{Key: key, Ascending: asc}

			lifetime := ca.AuthorsInput{WorkspaceID: ws, AccountID: integrationRef().AccountID, Sort: sortBy}
			windowed := lifetime
			windowed.From, windowed.To = &wideFrom, &now

			lifetimeOrder, lifetimeTotal := periodHandles(t, authors, lifetime)
			windowedOrder, windowedTotal := periodHandles(t, authors, windowed)

			if lifetimeTotal != windowedTotal {
				t.Fatalf("%s asc=%v: totals differ, %d vs %d", key, asc, lifetimeTotal, windowedTotal)
			}
			if !equalStrings(lifetimeOrder, windowedOrder) {
				t.Fatalf("%s asc=%v: orders differ\n lifetime = %v\n windowed = %v", key, asc, lifetimeOrder, windowedOrder)
			}
		}
	}
}

// The derived filters mean what they say INSIDE the window: flagged here is
// "flagged by what they did in this window", not "flagged all time".
func TestIntegration_WindowedRankingFiltersOnDerivedStanding(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	old := now.Add(-40 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)

	// Flagged by their history, but this week they only said nice things.
	seedAuthorComments(t, repo, ws, "ext-reformed", 5, ca.StanceHostile, shared.QualityLevelHigh, old)
	seedAuthorComments(t, repo, ws, "ext-reformed", 3, ca.StanceSupporter, shared.QualityLevelNone, recent)
	// Hostile this week, and severe enough to flag.
	seedAuthorComments(t, repo, ws, "ext-angry", 4, ca.StanceHostile, shared.QualityLevelHigh, recent)

	weekFrom := now.Add(-7 * 24 * time.Hour)
	flagged := ca.AuthorsInput{
		WorkspaceID: ws, AccountID: integrationRef().AccountID,
		From: &weekFrom, To: &now, FlaggedOnly: true, Sort: ca.DefaultAuthorSort,
	}
	got, total := periodHandles(t, authors, flagged)
	if total != 1 || len(got) != 1 || got[0] != "ext-angry" {
		t.Fatalf("flagged this week = %v (total %d), want only ext-angry", got, total)
	}

	supporters := flagged
	supporters.FlaggedOnly = false
	supporters.Stance = ca.StanceSupporter
	got, total = periodHandles(t, authors, supporters)
	if total != 1 || len(got) != 1 || got[0] != "ext-reformed" {
		t.Fatalf("supporters this week = %v (total %d), want ext-reformed", got, total)
	}
}

// Paging over a window must not repeat or skip a person, and the total must
// count PEOPLE rather than their comments.
func TestIntegration_WindowedRankingPagesOverPeople(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	at := now.Add(-24 * time.Hour)
	for i := 0; i < 5; i++ {
		seedAuthorComments(t, repo, ws, fmt.Sprintf("ext-%d", i), 3, ca.StanceNeutral, shared.QualityLevelNone, at)
	}

	from := now.Add(-7 * 24 * time.Hour)
	seen := map[string]int{}
	for page := 1; page <= 3; page++ {
		in := ca.AuthorsInput{
			WorkspaceID: ws, AccountID: integrationRef().AccountID,
			From: &from, To: &now, Sort: ca.DefaultAuthorSort,
			Options: shared.QueryOptions{Pagination: shared.Pagination{Page: page, PageSize: 2}},
		}
		got, total := periodHandles(t, authors, in)
		if total != 5 {
			t.Fatalf("total = %d, want 5 people rather than their 15 comments", total)
		}
		for _, h := range got {
			seen[h]++
		}
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d distinct people across three pages: %v", len(seen), seen)
	}
	for h, n := range seen {
		if n != 1 {
			t.Fatalf("%s appeared %d times", h, n)
		}
	}
}

// A person the operator muted keeps that state in a windowed ranking: it is
// theirs, not a function of the window.
func TestIntegration_WindowedRankingKeepsOperatorState(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	at := now.Add(-24 * time.Hour)
	seedAuthorComments(t, repo, ws, "ext-muted", 4, ca.StanceHostile, shared.QualityLevelHigh, at)

	rebuilt, err := repo.AggregateAuthors(context.Background(), ca.SourceInstagram, integrationRef().AccountID, now.Add(-365*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rebuilt {
		a.Derive()
	}
	if err := authors.UpsertMany(context.Background(), rebuilt); err != nil {
		t.Fatal(err)
	}
	// AggregateAuthors counts; it does not assign ids. The row id exists only
	// once the projection is written, so it is read back rather than assumed.
	projected, err := authors.List(context.Background(), lifetimeInput(ws))
	if err != nil || len(projected.Items) != 1 {
		t.Fatalf("projection: %v, %d rows", err, len(projected.Items))
	}
	authorID := projected.Items[0].ID
	if authorID == "" {
		t.Fatal("the projection must carry a row id")
	}
	if err := authors.SetModerationState(context.Background(), ws, authorID, ca.ModerationMuted, now); err != nil {
		t.Fatal(err)
	}

	from := now.Add(-7 * 24 * time.Hour)
	in := ca.AuthorsInput{
		WorkspaceID: ws, AccountID: integrationRef().AccountID,
		From: &from, To: &now, Sort: ca.DefaultAuthorSort,
	}
	in.Normalize()
	page, err := authors.List(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d", len(page.Items))
	}
	got := page.Items[0]
	if got.ModerationState != ca.ModerationMuted {
		t.Fatalf("moderation = %q, the operator's decision must survive the window", got.ModerationState)
	}
	// The row id has to come back too, or nothing in the UI can open, moderate
	// or escalate the person the ranking just listed.
	if got.ID != authorID {
		t.Fatalf("id = %q, want the author row id %q", got.ID, authorID)
	}

	// And the same state filters, since it is an ordinary column on the row.
	muted := in
	muted.ModerationState = ca.ModerationMuted
	if _, total := periodHandles(t, authors, muted); total != 1 {
		t.Fatalf("muted total = %d, want 1", total)
	}
	blocked := in
	blocked.ModerationState = ca.ModerationBlocked
	if _, total := periodHandles(t, authors, blocked); total != 0 {
		t.Fatalf("blocked total = %d, want 0", total)
	}
}

// lifetimeInput is the all-time ranking for one workspace, used to read a
// projection back after a rebuild.
func lifetimeInput(ws string) ca.AuthorsInput {
	in := ca.AuthorsInput{WorkspaceID: ws, AccountID: integrationRef().AccountID, Sort: ca.DefaultAuthorSort}
	in.Normalize()
	return in
}

// minComments filters people, and the total has to agree with the page.
func TestIntegration_WindowedRankingMinComments(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	authors := NewAuthorRepository(db)
	ws := "11111111-1111-1111-1111-111111111111"

	at := now.Add(-24 * time.Hour)
	seedAuthorComments(t, repo, ws, "ext-chatty", 6, ca.StanceCritic, shared.QualityLevelNone, at)
	seedAuthorComments(t, repo, ws, "ext-quiet", 2, ca.StanceCritic, shared.QualityLevelNone, at)

	from := now.Add(-7 * 24 * time.Hour)
	in := ca.AuthorsInput{
		WorkspaceID: ws, AccountID: integrationRef().AccountID,
		From: &from, To: &now, MinComments: 5, Sort: ca.DefaultAuthorSort,
	}
	got, total := periodHandles(t, authors, in)
	if total != 1 || len(got) != 1 || got[0] != "ext-chatty" {
		t.Fatalf("min 5 comments = %v (total %d), want only ext-chatty", got, total)
	}
}

// The window narrows the posts too, so the author dialog and the ranking agree
// about what "this week" means.
func TestIntegration_AuthorContainersRespectsTheWindow(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	ctx := context.Background()
	ws := "11111111-1111-1111-1111-111111111111"

	seedAnalyzedIn(t, repo, ws, "media-old", "ext-1", "s-old",
		classification(ca.StanceCritic, shared.SentimentNegative, shared.QualityLevelNone), now.Add(-40*24*time.Hour))
	seedAnalyzedIn(t, repo, ws, "media-new", "ext-1", "s-new",
		classification(ca.StanceCritic, shared.SentimentNegative, shared.QualityLevelNone), now.Add(-24*time.Hour))

	from := now.Add(-7 * 24 * time.Hour)
	in := ca.AuthorContainersInput{WorkspaceID: ws, AuthorExternalID: "ext-1", From: &from, To: &now}
	in.Normalize()
	page, err := repo.ListAuthorContainers(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 || page.Items[0].ContainerID != "media-new" {
		t.Fatalf("windowed posts = %+v, want only media-new", page.Items)
	}
}
