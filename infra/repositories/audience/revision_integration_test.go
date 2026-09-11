package audience_repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
)

// The revision SQL, against a real Postgres.
//
// Everything here is a claim sqlmock cannot check. A correlated NOT EXISTS
// with a row-value comparison, DISTINCT ON, a unique index and date_trunc
// grouping are all accepted as strings by a mock and rejected (or, worse,
// quietly answered wrongly) by a database. These are the queries the
// conversation timeline rests on, so they are exercised where the answer is
// real.

const revisionWorkspace = "33333333-3333-3333-3333-333333333333"

func conversationRef() ca.ContainerRef {
	return ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceWhatsApp,
		AccountID: "44444444-4444-4444-4444-444444444444", ContainerID: "camp-1",
	}
}

// insertRevision stores one snapshot of a conversation, already classified
// unless a status says otherwise.
func insertRevision(
	t *testing.T, repo ca.Repository, entryID, revision string, messages int, at time.Time, status ca.Status,
) *ca.Analysis {
	t.Helper()
	row, err := ca.NewPending(ca.NewInput{
		WorkspaceID: revisionWorkspace, Container: conversationRef(), SubjectID: entryID,
		AuthorExternalID: "contact-" + entryID, AuthorHandle: "contato",
		Text: fmt.Sprintf("transcricao %s", revision), OccurredAt: at, Now: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	row.ID = uuid.New().String()
	row.Revision, row.Transcript, row.MessageCount = revision, "transcricao "+revision, messages

	switch status {
	case ca.StatusAnalyzed:
		if err := row.Claim(at); err != nil {
			t.Fatal(err)
		}
		if err := row.Apply(ca.Classification{
			Sentiment: shared.SentimentPositive, Interest: ca.InterestInterested,
			Disposition: ca.DispositionSale, Qualification: ca.QualificationHotLead,
			NextAction: ca.NextActionClose, Summary: "Fechou.", Language: "pt",
			Quality: ca.ConversationQuality{
				GoalProgress: shared.QualityLevelHigh, CustomerEngagement: shared.QualityLevelHigh,
				AgentConduct: shared.QualityLevelHigh, Professionalism: shared.QualityLevelHigh,
			},
		}, ca.ActionPolicy{}, ca.Provenance{BatchID: uuid.New().String(), Model: "m"}, at); err != nil {
			t.Fatal(err)
		}
	case ca.StatusFailed:
		if err := row.Fail(ca.ReasonProviderError, at); err != nil {
			t.Fatal(err)
		}
	}

	inserted, err := repo.Insert(context.Background(), row)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatalf("revision %s of %s was refused as a duplicate", revision, entryID)
	}
	return row
}

// The unique index is on the REVISION, so a conversation accumulates a
// timeline of analyses while a redelivered snapshot is still refused.
func TestIntegration_RevisionsAccumulateButSnapshotsDoNot(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusAnalyzed)

	duplicate, err := ca.NewPending(ca.NewInput{
		WorkspaceID: revisionWorkspace, Container: conversationRef(), SubjectID: "entry-1",
		AuthorExternalID: "contact-entry-1", Text: "transcricao rev-b", Now: now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate.ID, duplicate.Revision = uuid.New().String(), "rev-b"
	inserted, err := repo.Insert(context.Background(), duplicate)
	if err != nil {
		t.Fatalf("a redelivered snapshot raised instead of being absorbed: %v", err)
	}
	if inserted {
		t.Error("the same revision was stored twice, so a retry would buy a second analysis")
	}

	var stored int64
	if err := db.Table("audience_analyses").Where("subject_id = ?", "entry-1").Count(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != 2 {
		t.Fatalf("stored %d rows for one conversation, want its 2 revisions", stored)
	}
}

// LatestOnly is what keeps a conversation from being counted once per analysis
// in every total on the dashboard.
func TestIntegration_LatestOnlyCountsAConversationOnce(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-2", "rev-a", 6, now.Add(2*time.Hour), ca.StatusAnalyzed)

	all, err := repo.List(context.Background(), ca.ListInput{WorkspaceID: revisionWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if all.TotalItems != 3 {
		t.Fatalf("the history holds %d rows, want all 3 analyses", all.TotalItems)
	}

	latest, err := repo.List(context.Background(), ca.ListInput{WorkspaceID: revisionWorkspace, LatestOnly: true})
	if err != nil {
		t.Fatalf("the latest-revision filter was rejected by Postgres: %v", err)
	}
	if latest.TotalItems != 2 {
		t.Fatalf("latest-only returned %d rows, want one per conversation", latest.TotalItems)
	}
	for _, row := range latest.Items {
		if row.SubjectID == "entry-1" && row.Revision != "rev-b" {
			t.Errorf("entry-1 came back as %q, want its newest revision", row.Revision)
		}
	}

	stats, err := repo.GetStats(context.Background(), ca.ListInput{WorkspaceID: revisionWorkspace, LatestOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Counters.ConversationAnalyzed != 2 {
		t.Errorf("stats counted %d analysed conversations, want 2", stats.Counters.ConversationAnalyzed)
	}
	if stats.Counters.LastAnalyzedAt == nil {
		t.Error("no freshness timestamp, so the dashboard cannot say when it last moved")
	}
}

// A period's totals must describe that period. The newer revision is outside
// it, so the conversation is counted at the verdict it held AT the time.
func TestIntegration_LatestOnlyRespectsThePeriodEnd(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(48*time.Hour), ca.StatusAnalyzed)

	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	page, err := repo.List(context.Background(), ca.ListInput{
		WorkspaceID: revisionWorkspace, LatestOnly: true, From: &from, To: &to,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 {
		t.Fatalf("the period returned %d rows, want 1", page.TotalItems)
	}
	if page.Items[0].Revision != "rev-a" {
		t.Errorf("the period shows %q, want the revision that was current then", page.Items[0].Revision)
	}
}

// The daily series groups by UTC day over the live rows, which is the path a
// workspace-wide or mixed-channel view takes because it has no single rollup.
func TestIntegration_GetTrendGroupsAnalysesByUTCDay(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	day1 := time.Date(2026, 9, 2, 23, 30, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 3, 0, 30, 0, 0, time.UTC)
	insertRevision(t, repo, "entry-1", "rev-a", 4, day1, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, day2, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-2", "rev-a", 5, day2, ca.StatusAnalyzed)
	// Still queued: a trend point is what was learned, not what is waiting.
	insertRevision(t, repo, "entry-3", "rev-a", 3, day2, ca.StatusPending)

	rows, err := repo.GetTrend(context.Background(), ca.ListInput{
		WorkspaceID:  revisionWorkspace,
		SubjectKinds: []ca.SubjectKind{ca.SubjectKindConversation},
	})
	if err != nil {
		t.Fatalf("the trend query was rejected by Postgres: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d buckets, want one per day", len(rows))
	}
	wantFirst := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	if !time.Time(rows[0].BucketDate).Equal(wantFirst) {
		t.Errorf("first bucket is %v, want %v", time.Time(rows[0].BucketDate), wantFirst)
	}
	if rows[0].Counters.ConversationAnalyzed != 1 || rows[1].Counters.ConversationAnalyzed != 2 {
		t.Errorf("buckets counted %d and %d, want 1 and 2 (the pending row is not an analysis)",
			rows[0].Counters.ConversationAnalyzed, rows[1].Counters.ConversationAnalyzed)
	}
}

// The inbox shows a verdict, so it reads the newest ANALYSED revision. A newer
// one still in the queue must not blank out the answer already on the screen,
// which is what "latest row wins" would do.
func TestIntegration_ConversationReaderSkipsUnfinishedRevisions(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-c", 14, now.Add(2*time.Hour), ca.StatusPending)
	insertRevision(t, repo, "entry-2", "rev-a", 6, now, ca.StatusFailed)

	reader := NewConversationReader(db)
	one, err := reader.LatestByEntry(context.Background(), revisionWorkspace, ca.SourceWhatsApp, "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if one.Revision != "rev-b" {
		t.Errorf("LatestByEntry returned %q, want the newest analysed revision", one.Revision)
	}

	many, err := reader.LatestByEntries(context.Background(), revisionWorkspace, ca.SourceWhatsApp, []string{"entry-1", "entry-2"})
	if err != nil {
		t.Fatalf("the DISTINCT ON query was rejected by Postgres: %v", err)
	}
	if got := many["entry-1"]; got == nil || got.Revision != "rev-b" {
		t.Errorf("entry-1 = %+v, want rev-b", got)
	}
	if _, ok := many["entry-2"]; ok {
		t.Error("a conversation whose only analysis failed was reported as having a verdict")
	}
}

// The billing guard's lookup is the opposite question: it wants the newest row
// whatever state it is in, because a snapshot already waiting in the queue is
// exactly what must stop another from being paid for.
func TestIntegration_LatestBySubjectSeesUnfinishedWork(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusPending)

	latest, err := repo.LatestBySubject(context.Background(), revisionWorkspace, ca.SourceWhatsApp, ca.SubjectKindConversation, "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Revision != "rev-b" || latest.Status != ca.StatusPending {
		t.Fatalf("latest = %+v, want the queued rev-b", latest)
	}

	none, err := repo.LatestBySubject(context.Background(), revisionWorkspace, ca.SourceWhatsApp, ca.SubjectKindConversation, "entry-never-seen")
	if err != nil {
		t.Fatalf("an unseen conversation was an error rather than an absence: %v", err)
	}
	if none != nil {
		t.Fatalf("an unseen conversation returned %+v", none)
	}
}

// A database deployed before revisions carries the old unique index, which
// allows exactly one analysis per conversation. The migration has to remove it,
// or every second analysis is silently dropped on conflict forever.
func TestIntegration_MigrationReplacesThePreRevisionIndex(t *testing.T) {
	db := integrationDB(t)
	if err := db.Exec(`DROP INDEX ux_ca_revision`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX ux_ca_subject ON audience_analyses (source, subject_kind, subject_id)`).Error; err != nil {
		t.Fatal(err)
	}

	if err := applyAudienceIndexesForTest(db); err != nil {
		t.Fatalf("the migration failed on a database that had the old index: %v", err)
	}

	repo := NewRepository(db)
	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusAnalyzed)
}

// applyAudienceIndexesForTest runs the statements the schema migration runs for
// this table. Kept beside the migration's own list on purpose: if they drift,
// the test above stops describing what a deploy actually does.
func applyAudienceIndexesForTest(db *gorm.DB) error {
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_ca_revision
			ON audience_analyses (source, subject_kind, subject_id, revision)`,
		`DROP INDEX IF EXISTS ux_ca_subject`,
		`CREATE INDEX IF NOT EXISTS idx_ca_latest_revision ON audience_analyses
			(workspace_id, source, subject_id, occurred_at DESC, created_at DESC, id DESC)
			WHERE subject_kind = 'conversation' AND deleted_at IS NULL`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// insertSubject stores one analysed conversation whose subject is exactly what
// the model wrote, spelling and all.
func insertSubject(t *testing.T, repo ca.Repository, entryID, revision, subject string, at time.Time) {
	t.Helper()
	row, err := ca.NewPending(ca.NewInput{
		WorkspaceID: revisionWorkspace, Container: conversationRef(), SubjectID: entryID,
		AuthorExternalID: "contact-" + entryID, Text: "transcricao", OccurredAt: at, Now: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	row.ID, row.Revision, row.Transcript, row.MessageCount = uuid.New().String(), revision, "transcricao", 6
	if err := row.Claim(at); err != nil {
		t.Fatal(err)
	}
	if err := row.Apply(ca.Classification{
		Sentiment: shared.SentimentPositive, Interest: ca.InterestInterested,
		Disposition: ca.DispositionSale, Qualification: ca.QualificationHotLead,
		NextAction: ca.NextActionClose, ProductInterest: subject,
		Summary: "Fechou.", Language: "pt",
		Quality: ca.ConversationQuality{
			GoalProgress: shared.QualityLevelHigh, CustomerEngagement: shared.QualityLevelHigh,
			AgentConduct: shared.QualityLevelHigh, Professionalism: shared.QualityLevelHigh,
		},
	}, ca.ActionPolicy{}, ca.Provenance{BatchID: uuid.New().String(), Model: "m"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert(context.Background(), row); err != nil {
		t.Fatal(err)
	}
}

// The subject ranking, end to end through the database.
//
// The whole feature turns on spellings colliding in SQL, which is precisely
// what cannot be checked without a database: the key is computed in Go but
// GROUPed in Postgres, and a column that is never written or a GROUP BY on the
// wrong one both produce a plausible-looking empty chart.
func TestIntegration_SubjectRankingGroupsSpellingsTogether(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	// One subject, written four ways by the model.
	insertSubject(t, repo, "entry-1", "rev-a", "Plano Família", now)
	insertSubject(t, repo, "entry-2", "rev-a", "plano familia", now)
	insertSubject(t, repo, "entry-3", "rev-a", "  PLANO  FAMÍLIA ", now)
	insertSubject(t, repo, "entry-4", "rev-a", "Plano família!", now)
	// A second subject, and one conversation with none.
	insertSubject(t, repo, "entry-5", "rev-a", "clareamento dental", now)
	insertSubject(t, repo, "entry-6", "rev-a", "clareamento dental", now)
	insertSubject(t, repo, "entry-7", "rev-a", "n/a", now)

	stats, err := repo.GetStats(context.Background(), ca.ListInput{WorkspaceID: revisionWorkspace})
	if err != nil {
		t.Fatalf("the subject ranking query was rejected by Postgres: %v", err)
	}
	if len(stats.Subjects) != 2 {
		t.Fatalf("ranked %d subjects, want 2: %+v", len(stats.Subjects), stats.Subjects)
	}
	if stats.Subjects[0].Key != ca.SubjectKey("plano familia") || stats.Subjects[0].Count != 4 {
		t.Errorf("top subject = %+v, want the four spellings of plano familia counted as one", stats.Subjects[0])
	}
	if stats.Subjects[1].Count != 2 {
		t.Errorf("second subject = %+v, want 2", stats.Subjects[1])
	}
	// The label is for reading, so it must be a real spelling rather than the
	// stripped key.
	if stats.Subjects[0].Label == "" {
		t.Error("the top subject has no label to print")
	}
}

// A subject the model could not name is not a subject. Counting "n/a" would
// put a meaningless bar at the top of the chart on any workspace whose
// conversations are mostly small talk.
func TestIntegration_SubjectRankingIgnoresAbsentSubjects(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	for i, blank := range []string{"", "  ", "-", "n/a", "nenhum"} {
		insertSubject(t, repo, fmt.Sprintf("entry-%d", i), "rev-a", blank, now)
	}

	stats, err := repo.GetStats(context.Background(), ca.ListInput{WorkspaceID: revisionWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Subjects) != 0 {
		t.Errorf("ranked %+v, want nothing", stats.Subjects)
	}
}

// The ranking answers over the SAME filters as the numbers above it, so a
// period that excludes a conversation excludes its subject too.
func TestIntegration_SubjectRankingRespectsTheFilters(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	insertSubject(t, repo, "entry-1", "rev-a", "plano familia", now)
	insertSubject(t, repo, "entry-2", "rev-a", "clareamento dental", now.Add(48*time.Hour))

	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	stats, err := repo.GetStats(context.Background(), ca.ListInput{
		WorkspaceID: revisionWorkspace, From: &from, To: &to,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Subjects) != 1 || stats.Subjects[0].Key != ca.SubjectKey("plano familia") {
		t.Errorf("the period ranked %+v, want only the subject inside it", stats.Subjects)
	}
}

// Pending has to survive a reload, which means it comes from the database and
// not only from the socket that announced it.
func TestIntegration_PendingByEntriesReportsWaitingWork(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)

	// Analysed once, and a newer revision now queued: BOTH facts are true of
	// this conversation at the same time, which is the case the inbox has to
	// render as "here is the verdict, and a new one is coming".
	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusAnalyzed)
	insertRevision(t, repo, "entry-1", "rev-b", 9, now.Add(time.Hour), ca.StatusPending)
	// Analysed and settled: nothing waiting.
	insertRevision(t, repo, "entry-2", "rev-a", 5, now, ca.StatusAnalyzed)
	// Never analysed, still queued.
	insertRevision(t, repo, "entry-3", "rev-a", 3, now, ca.StatusPending)
	// Failed: not waiting on anything, it is over.
	insertRevision(t, repo, "entry-4", "rev-a", 6, now, ca.StatusFailed)

	reader := NewConversationReader(db)
	pending, err := reader.PendingByEntries(context.Background(), revisionWorkspace, ca.SourceWhatsApp,
		[]string{"entry-1", "entry-2", "entry-3", "entry-4"})
	if err != nil {
		t.Fatalf("PendingByEntries: %v", err)
	}

	if !pending["entry-1"] {
		t.Error("a conversation being re-analysed was not reported as pending")
	}
	if !pending["entry-3"] {
		t.Error("a conversation waiting for its first analysis was not reported as pending")
	}
	if pending["entry-2"] {
		t.Error("a settled conversation was reported as pending")
	}
	if pending["entry-4"] {
		t.Error("a failed analysis was reported as still waiting")
	}
	// Absent, not false: the map is the size of the answer, not of the page.
	if len(pending) != 2 {
		t.Errorf("map holds %d entries, want only the two with work waiting", len(pending))
	}

	// The verdict is unaffected: a queued revision must not blank out the
	// answer already on the screen.
	verdicts, err := reader.LatestByEntries(context.Background(), revisionWorkspace, ca.SourceWhatsApp, []string{"entry-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := verdicts["entry-1"]; got == nil || got.Revision != "rev-a" {
		t.Errorf("entry-1 verdict = %+v, want its last completed revision", got)
	}
}

// Another workspace's queue is not this one's. The inbox passes entry ids it
// already resolved, but the scope still travels, so a shared id can never leak
// a pending marker across a tenant boundary.
func TestIntegration_PendingByEntriesStaysInItsWorkspace(t *testing.T) {
	db := integrationDB(t)
	repo := NewRepository(db)
	insertRevision(t, repo, "entry-1", "rev-a", 4, now, ca.StatusPending)

	reader := NewConversationReader(db)
	pending, err := reader.PendingByEntries(context.Background(),
		"99999999-9999-9999-9999-999999999999", ca.SourceWhatsApp, []string{"entry-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("another workspace saw %v", pending)
	}
}
