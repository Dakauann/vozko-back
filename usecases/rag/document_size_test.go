package rag_usecase

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"vozko/domain/rag"
)

type sizedKB struct {
	rag.KnowledgeBaseRepository
	kb    *rag.KnowledgeBase
	added []int64
}

func (k *sizedKB) FindByID(context.Context, string) (*rag.KnowledgeBase, error) { return k.kb, nil }
func (k *sizedKB) IncrementDocumentCount(context.Context, string, int) error    { return nil }
func (k *sizedKB) AddTotalSize(_ context.Context, _ string, delta int64) error {
	k.added = append(k.added, delta)
	return nil
}

func sizedBase(totalMB float64) *sizedKB {
	return &sizedKB{kb: &rag.KnowledgeBase{ID: "kb1", WorkspaceID: "ws1", Status: rag.KnowledgeBaseStatusActive, TotalSizeMB: totalMB}}
}

func TestCreatedDocumentRecordsTheRealFileSizeOnTheBase(t *testing.T) {
	kb := sizedBase(0)
	file := strings.Repeat("x", 3000)
	doc, err := NewCreateDocumentUseCase(&docRepoStub{}, kb, nil, nil).Execute(context.Background(), rag.CreateDocumentInput{
		KnowledgeBaseID: "kb1", Name: "faq.pdf", Type: rag.DocumentTypePDF,
		Content:  base64.StdEncoding.EncodeToString([]byte(file)),
		Metadata: map[string]string{rag.MetadataEncoding: rag.EncodingBase64},
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.SizeBytes != 3000 || len(kb.added) != 1 || kb.added[0] != 3000 {
		t.Fatalf("size %d added %v", doc.SizeBytes, kb.added)
	}
}

func TestCreateDocumentRefusesPastTheBaseSizeLimit(t *testing.T) {
	kb := sizedBase(float64(rag.MaxTotalSizeMB) - 0.001)
	_, err := NewCreateDocumentUseCase(&docRepoStub{}, kb, nil, nil).Execute(context.Background(), rag.CreateDocumentInput{
		KnowledgeBaseID: "kb1", Name: "big.txt", Type: rag.DocumentTypeText, Content: strings.Repeat("x", 2048),
	})
	if !errors.Is(err, rag.ErrMaxTotalSizeReached) || len(kb.added) != 0 {
		t.Fatalf("err %v added %v", err, kb.added)
	}
}

type sizedDocs struct {
	rag.DocumentRepository
	doc *rag.Document
}

func (d sizedDocs) FindByID(context.Context, string) (*rag.Document, error) { return d.doc, nil }
func (d sizedDocs) Delete(context.Context, string) error                   { return nil }

type chunkParts struct{ rag.ChunkRepository }

func (chunkParts) DeleteByDocument(context.Context, string) error { return nil }

type vectorParts struct{ rag.VectorRepository }

func (vectorParts) DeleteByDocument(context.Context, string) error { return nil }

func TestDeletedDocumentGivesItsSizeBack(t *testing.T) {
	kb := sizedBase(1)
	uc := NewDeleteDocumentUseCase(sizedDocs{doc: &rag.Document{ID: "d1", KnowledgeBaseID: "kb1", SizeBytes: 4096}}, chunkParts{}, vectorParts{}, kb)
	if err := uc.Execute(context.Background(), "d1"); err != nil {
		t.Fatal(err)
	}
	if len(kb.added) != 1 || kb.added[0] != -4096 {
		t.Fatalf("added %v", kb.added)
	}
}
