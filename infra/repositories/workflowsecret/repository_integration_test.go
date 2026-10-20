package workflowsecret_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflowsecret"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestPostgresUpsertKeepsOneRowPerWorkspaceNameAndTheFirstCreator(t *testing.T) {
	db := repotest.IsolatedDB(t, "wfsecret", &schema.WorkflowSecret{})
	repo := New(db)
	ctx := context.Background()
	ws := uuid.NewString()
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	later := first.Add(time.Hour)

	if _, err := repo.Upsert(ctx, workflowsecret.Secret{ID: uuid.NewString(), WorkspaceID: ws, Name: "senha", EncryptedValue: []byte("v1"), CreatedBy: "u1", UpdatedBy: "u1", CreatedAt: first, UpdatedAt: first}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Upsert(ctx, workflowsecret.Secret{ID: uuid.NewString(), WorkspaceID: ws, Name: "senha", EncryptedValue: []byte("v2"), CreatedBy: "u2", UpdatedBy: "u2", CreatedAt: later, UpdatedAt: later})
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedBy != "u1" || got.UpdatedBy != "u2" {
		t.Fatalf("got %+v", got)
	}
	rows, err := repo.FindByNames(ctx, ws, []string{"senha", "absent"})
	if err != nil || len(rows) != 1 || string(rows[0].EncryptedValue) != "v2" {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	if other, _ := repo.FindByNames(ctx, uuid.NewString(), []string{"senha"}); len(other) != 0 {
		t.Fatal("another workspace must not see the secret")
	}
	list, err := repo.List(ctx, ws)
	if err != nil || len(list) != 1 || list[0].UpdatedBy != "u2" {
		t.Fatalf("list %+v err %v", list, err)
	}
	if err := repo.Delete(ctx, ws, "senha"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, ws, "senha"); !errors.Is(err, workflowsecret.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}
