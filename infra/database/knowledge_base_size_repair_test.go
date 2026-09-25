package database

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"vozko/infra/database/schema"
)

func TestRepairSizesDocumentsFromTheirContentAndTotalsEachBase(t *testing.T) {
	tx := repairTx(t)
	if err := tx.AutoMigrate(&schema.KnowledgeBase{}, &schema.RAGDocument{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	kb := schema.KnowledgeBase{ID: uuid.New().String(), WorkspaceID: uuid.New().String(), Name: "Políticas"}
	empty := schema.KnowledgeBase{ID: uuid.New().String(), WorkspaceID: kb.WorkspaceID, Name: "Vazia", TotalSizeMB: 3}
	for _, row := range []*schema.KnowledgeBase{&kb, &empty} {
		if err := tx.Create(row).Error; err != nil {
			t.Fatalf("kb: %v", err)
		}
	}
	file := strings.Repeat("p", 1024*1024+1)
	encoded := base64.StdEncoding.EncodeToString([]byte(file))
	doc := func(content, metadata string, size int64) string {
		row := schema.RAGDocument{ID: uuid.New().String(), KnowledgeBaseID: kb.ID, Name: "d", Content: content, Metadata: metadata, SizeBytes: size}
		if err := tx.Create(&row).Error; err != nil {
			t.Fatalf("doc: %v", err)
		}
		return row.ID
	}
	pdf := doc(encoded, `{"encoding":"base64"}`, int64(len(encoded)))
	text := doc("preço", `{}`, 0)
	gone := doc("apagado", `{}`, 0)
	if err := tx.Delete(&schema.RAGDocument{}, "id = ?", gone).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	for range 2 {
		if err := sizeKnowledgeBaseDocuments(tx); err != nil {
			t.Fatalf("repair: %v", err)
		}
	}

	for id, want := range map[string]int64{pdf: int64(len(file)), text: int64(len("preço"))} {
		var row schema.RAGDocument
		tx.First(&row, "id = ?", id)
		if row.SizeBytes != want {
			t.Fatalf("document %s size %d, want %d", id, row.SizeBytes, want)
		}
	}
	var got, none schema.KnowledgeBase
	tx.First(&got, "id = ?", kb.ID)
	tx.First(&none, "id = ?", empty.ID)
	wantMB := float64(len(file)+len("preço")) / (1024 * 1024)
	if diff := got.TotalSizeMB - wantMB; diff > 1e-9 || diff < -1e-9 || none.TotalSizeMB != 0 {
		t.Fatalf("totals = %v and %v, want %v and 0", got.TotalSizeMB, none.TotalSizeMB, wantMB)
	}
}

func TestKnowledgeBaseSizeRepairIsRegistered(t *testing.T) {
	if !slices.Contains(repairNames(), "rag_size_documents_and_bases") {
		t.Fatalf("the repair is not run at boot")
	}
}
