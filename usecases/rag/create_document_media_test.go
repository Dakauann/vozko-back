package rag_usecase

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"vozko/domain/media"
	"vozko/domain/rag"
)

type docRepoStub struct {
	rag.DocumentRepository
	created []*rag.Document
}

func (r *docRepoStub) CountByKnowledgeBase(context.Context, string) (int, error) { return 0, nil }
func (r *docRepoStub) Create(_ context.Context, d *rag.Document) error {
	r.created = append(r.created, d)
	return nil
}

type kbByID struct {
	rag.KnowledgeBaseRepository
	kb *rag.KnowledgeBase
}

func (k kbByID) FindByID(context.Context, string) (*rag.KnowledgeBase, error) { return k.kb, nil }
func (k kbByID) IncrementDocumentCount(context.Context, string, int) error    { return nil }
func (k kbByID) AddTotalSize(context.Context, string, int64) error            { return nil }

type mediaReaderStub struct {
	asked string
	err   error
}

func (m *mediaReaderStub) Read(_ context.Context, workspaceID, mediaID string) (*media.Content, error) {
	m.asked = workspaceID + "|" + mediaID
	if m.err != nil {
		return nil, m.err
	}
	return &media.Content{Name: "tabela.pdf", Data: []byte("%PDF")}, nil
}

func activeKB() kbByID {
	return kbByID{kb: &rag.KnowledgeBase{ID: "kb1", WorkspaceID: "ws1", Status: rag.KnowledgeBaseStatusActive}}
}

func TestDocumentFromMediaReadsTheFileOfTheBasesWorkspace(t *testing.T) {
	docs, reader := &docRepoStub{}, &mediaReaderStub{}
	doc, err := NewCreateDocumentUseCase(docs, activeKB(), nil, reader).Execute(context.Background(), rag.CreateDocumentInput{
		KnowledgeBaseID: "kb1", Name: "Preços", Type: rag.DocumentTypePDF, MediaID: "m1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.asked != "ws1|m1" || doc.Name != "Preços.pdf" || doc.Metadata["encoding"] != "base64" {
		t.Fatalf("asked %q doc %+v", reader.asked, doc)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(doc.Content); string(decoded) != "%PDF" {
		t.Fatalf("content = %q", doc.Content)
	}
}

func TestDocumentFromAnotherWorkspacesMediaIsRefused(t *testing.T) {
	docs := &docRepoStub{}
	_, err := NewCreateDocumentUseCase(docs, activeKB(), nil, &mediaReaderStub{err: media.ErrMediaNotFound}).Execute(context.Background(), rag.CreateDocumentInput{
		KnowledgeBaseID: "kb1", Name: "x", Type: rag.DocumentTypePDF, MediaID: "m9",
	})
	if !errors.Is(err, media.ErrMediaNotFound) || len(docs.created) != 0 {
		t.Fatalf("err %v created %d", err, len(docs.created))
	}
}

func TestDocumentFromMediaTakesItsTypeFromTheFileName(t *testing.T) {
	doc, err := NewCreateDocumentUseCase(&docRepoStub{}, activeKB(), nil, &mediaReaderStub{}).Execute(context.Background(), rag.CreateDocumentInput{
		KnowledgeBaseID: "kb1", MediaID: "m1",
	})
	if err != nil || doc.Type != rag.DocumentTypePDF || doc.Name != "tabela.pdf" {
		t.Fatalf("doc %+v err %v", doc, err)
	}
}
