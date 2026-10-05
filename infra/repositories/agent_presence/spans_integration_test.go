package agent_presence_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	ap "vozko/domain/agent_presence"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestSpansReturnsTheMembersConnectedIntervalsThatTouchThePeriod(t *testing.T) {
	db := repotest.IsolatedDB(t, "presence_spans", &schema.AgentPresenceInterval{})
	repo := New(db)
	ws, member, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	ended := func(h int) *time.Time { t := base.Add(time.Duration(h) * time.Hour); return &t }
	rows := []schema.AgentPresenceInterval{
		{ID: uuid.NewString(), WorkspaceID: ws, UserID: member, State: string(ap.StateOnline), StartedAt: base.Add(-30 * time.Hour), EndedAt: ended(-26)},
		{ID: uuid.NewString(), WorkspaceID: ws, UserID: member, State: string(ap.StateOnline), StartedAt: base.Add(-2 * time.Hour), EndedAt: ended(1)},
		{ID: uuid.NewString(), WorkspaceID: ws, UserID: member, State: string(ap.StateOnCall), StartedAt: base.Add(1 * time.Hour), EndedAt: ended(2)},
		{ID: uuid.NewString(), WorkspaceID: ws, UserID: member, State: string(ap.StateOnline), StartedAt: base.Add(3 * time.Hour)},
		{ID: uuid.NewString(), WorkspaceID: ws, UserID: other, State: string(ap.StateOnline), StartedAt: base},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	spans, err := repo.Spans(ws, member, base.Add(-24*time.Hour), base.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 3 || spans[0].State != ap.StateOnline || spans[1].State != ap.StateOnCall || spans[2].EndedAt != nil {
		t.Fatalf("spans %+v", spans)
	}
}
