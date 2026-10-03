package call_cdr_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/calls/cdr"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var base = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

var (
	ana  = uuid.NewString()
	bia  = uuid.NewString()
	caio = uuid.NewString()
)

func ptr[T any](v T) *T { return &v }

func seedCall(t *testing.T, repo cdr.Repository, workspaceID, callID, agent, to string, minutesAgo int, answered bool) {
	t.Helper()
	call := &cdr.Call{
		CallID: callID, WorkspaceID: workspaceID, Type: cdr.CallTypeCRM, Direction: cdr.DirectionOutbound,
		Source: cdr.SourceSIPTrunk, Status: cdr.StatusInProgress, PhoneTo: to, StartedAt: base.Add(-time.Duration(minutesAgo) * time.Minute),
	}
	if agent != "" {
		call.AgentID = ptr(agent)
	}
	if err := repo.Create(call); err != nil {
		t.Fatalf("Create %s: %v", callID, err)
	}
	if answered {
		if err := repo.MarkAnswered(callID, call.StartedAt.Add(5*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTransfer(t *testing.T, db *gorm.DB, workspaceID, callID, from, target, answeredBy string) {
	t.Helper()
	row := schema.CallTransfer{
		ID: uuid.NewString(), WorkspaceID: workspaceID, CallID: callID, FromUserID: from,
		TargetKind: "member", TargetUserID: target, Outcome: "connected", AnsweredBy: answeredBy, CreatedAt: base,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func callIDs(page *shared.PaginatedResult[*cdr.Call]) []string {
	ids := make([]string, 0, len(page.Items))
	for _, call := range page.Items {
		ids = append(ids, call.CallID)
	}
	return ids
}

func assertIDs(t *testing.T, label string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func TestListingShowsAnOperatorEveryCallTheyTookPartIn(t *testing.T) {
	db := repotest.IsolatedDB(t, "calls", &schema.Call{}, &schema.CallTransfer{})
	repo := NewRepository(db)
	ws, other := uuid.NewString(), uuid.NewString()
	seedCall(t, repo, ws, "placed-by-ana", ana, "5584994409684", 1, true)
	seedCall(t, repo, ws, "sent-to-ana", bia, "5511999990000", 2, true)
	seedCall(t, repo, ws, "answered-by-ana", caio, "5511888880000", 3, true)
	seedCall(t, repo, ws, "not-ana", bia, "5511777770000", 4, false)
	seedCall(t, repo, other, "other-workspace", ana, "5584994409684", 5, true)
	seedTransfer(t, db, ws, "sent-to-ana", bia, ana, "")
	seedTransfer(t, db, ws, "answered-by-ana", caio, "", ana)
	seedTransfer(t, db, other, "not-ana", bia, ana, ana)

	mine, err := repo.List(cdr.ListFilters{WorkspaceID: ws, ParticipantID: ptr(ana)})
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, "ana's calls", callIDs(mine), "placed-by-ana", "sent-to-ana", "answered-by-ana")
	if mine.TotalItems != 3 {
		t.Fatalf("total = %d, want 3", mine.TotalItems)
	}

	everyone, _ := repo.List(cdr.ListFilters{WorkspaceID: ws})
	assertIDs(t, "workspace calls", callIDs(everyone), "placed-by-ana", "sent-to-ana", "answered-by-ana", "not-ana")
}

func TestListingFiltersByAnswerAndNumber(t *testing.T) {
	db := repotest.IsolatedDB(t, "calls", &schema.Call{}, &schema.CallTransfer{})
	repo := NewRepository(db)
	ws := uuid.NewString()
	seedCall(t, repo, ws, "answered", ana, "5584994409684", 1, true)
	seedCall(t, repo, ws, "missed", ana, "5511999990000", 2, false)

	answered, _ := repo.List(cdr.ListFilters{WorkspaceID: ws, Answered: ptr(true)})
	assertIDs(t, "answered", callIDs(answered), "answered")
	unanswered, _ := repo.List(cdr.ListFilters{WorkspaceID: ws, Answered: ptr(false)})
	assertIDs(t, "unanswered", callIDs(unanswered), "missed")
	byNumber, _ := repo.List(cdr.ListFilters{WorkspaceID: ws, NumberDigits: ptr("99440")})
	assertIDs(t, "by number", callIDs(byNumber), "answered")
}

func TestOnlyCallsOpenPastTheCutoffAreStale(t *testing.T) {
	db := repotest.IsolatedDB(t, "calls", &schema.Call{})
	repo := NewRepository(db)
	ws := uuid.NewString()
	seedCall(t, repo, ws, "old-open", ana, "1", 400, false)
	seedCall(t, repo, ws, "recent-open", ana, "2", 10, false)
	seedCall(t, repo, ws, "old-closed", ana, "3", 500, true)
	ended := base.Add(-490 * time.Minute)
	if err := repo.Complete(cdr.CompleteInput{CallID: "old-closed", Status: cdr.StatusCompleted, EndedAt: &ended}); err != nil {
		t.Fatal(err)
	}

	stale, err := repo.ListStale(base.Add(-5*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].CallID != "old-open" {
		t.Fatalf("stale = %+v", stale)
	}
}
