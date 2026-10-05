package assignment_history_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestListByActorReturnsWhatWasHandedToThePersonInThePeriod(t *testing.T) {
	db := repotest.IsolatedDB(t, "history_by_actor", &schema.AssignmentHistory{})
	repo := New(db)
	ws, member := uuid.NewString(), uuid.NewString()
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for _, h := range []*ia.AssignmentHistory{
		{WorkspaceID: ws, EntryID: uuid.NewString(), EntryType: "whatsapp", ActorKind: "human", AssignedActorID: member, Trigger: ia.TriggerInboundRR, StartedAt: base},
		{WorkspaceID: ws, EntryID: uuid.NewString(), EntryType: "whatsapp", ActorKind: "human", AssignedActorID: member, Trigger: ia.TriggerManual, StartedAt: base.Add(time.Hour)},
		{WorkspaceID: ws, EntryID: uuid.NewString(), EntryType: "whatsapp", ActorKind: "human", AssignedActorID: member, Trigger: ia.TriggerInboundRR, StartedAt: base.Add(-72 * time.Hour)},
		{WorkspaceID: ws, EntryID: uuid.NewString(), EntryType: "whatsapp", ActorKind: "human", AssignedActorID: uuid.NewString(), Trigger: ia.TriggerInboundRR, StartedAt: base},
	} {
		if err := repo.Append(h); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.ListByActor(ws, member, base.Add(-24*time.Hour), base.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Trigger != ia.TriggerInboundRR || got[1].Trigger != ia.TriggerManual {
		t.Fatalf("history %+v", got)
	}
}
