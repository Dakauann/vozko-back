package rag

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/rag"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestEditingABaseKeepsTheCountersOtherWritersMoved(t *testing.T) {
	db := repotest.IsolatedDB(t, "rag_kb", &schema.KnowledgeBase{})
	repo := NewKnowledgeBaseRepository(db)
	ctx := context.Background()
	kb := &rag.KnowledgeBase{ID: uuid.NewString(), WorkspaceID: uuid.NewString(), Name: "Manual", Status: rag.KnowledgeBaseStatusActive}
	if err := repo.Create(ctx, kb); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.FindByID(ctx, kb.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.IncrementDocumentCount(ctx, kb.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.IncrementChunkCount(ctx, kb.ID, 4); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddTotalSize(ctx, kb.ID, 2<<20); err != nil {
		t.Fatal(err)
	}
	stale.Name = "Manual do cliente"
	if err := repo.Update(ctx, stale); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByID(ctx, kb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Manual do cliente" || got.DocumentCount != 1 || got.ChunkCount != 4 || got.TotalSizeMB != 2 {
		t.Fatalf("base = %+v, want the new name and the counters moved after it was read", got)
	}
}
