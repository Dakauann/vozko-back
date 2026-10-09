package aichat_repository

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAThreadRemembersWhetherItsCostIsTracked(t *testing.T) {
	repo := NewThreadRepository(repotest.IsolatedDB(t, "chat_cost_test", &schema.AIChatThread{}))
	tracked := &aichat.Thread{ID: uuid.New().String(), WorkspaceID: uuid.New().String(), UserID: uuid.New().String(), CostTracked: true}
	older := &aichat.Thread{ID: uuid.New().String(), WorkspaceID: tracked.WorkspaceID, UserID: tracked.UserID}
	for _, th := range []*aichat.Thread{tracked, older} {
		if err := repo.Create(th); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetByID(tracked.ID)
	if err != nil || !got.CostTracked {
		t.Fatalf("a tracked thread must load as tracked, got %+v %v", got, err)
	}
	got, err = repo.GetByID(older.ID)
	if err != nil || got.CostTracked {
		t.Fatalf("a thread without the flag must load as untracked, got %+v %v", got, err)
	}
}
