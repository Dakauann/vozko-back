package rag

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrDocumentNotFound              = errors.New("document not found")
	ErrDocumentIDRequired            = errors.New("document id is required")
	ErrDocumentKnowledgeBaseRequired = errors.New("knowledge base id is required")
	ErrDocumentNameRequired          = errors.New("document name is required")
	ErrDocumentContentRequired       = errors.New("document content is required")
	ErrDocumentTypeInvalid           = errors.New("document type is invalid")
)

type DocumentType string

const (
	DocumentTypeText     DocumentType = "text"
	DocumentTypePDF      DocumentType = "pdf"
	DocumentTypeDocx     DocumentType = "docx"
	DocumentTypeMarkdown DocumentType = "markdown"
	DocumentTypeHTML     DocumentType = "html"
	DocumentTypeJSON     DocumentType = "json"
)

func (t DocumentType) IsValid() bool {
	switch t {
	case DocumentTypeText, DocumentTypePDF, DocumentTypeDocx, DocumentTypeMarkdown, DocumentTypeHTML, DocumentTypeJSON:
		return true
	default:
		return false
	}
}

type DocumentStatus string

const (
	DocumentStatusPending    DocumentStatus = "pending"
	DocumentStatusProcessing DocumentStatus = "processing"
	DocumentStatusReady      DocumentStatus = "ready"
	DocumentStatusFailed     DocumentStatus = "failed"
)

type Document struct {
	ID              string            `json:"id"`
	KnowledgeBaseID string            `json:"knowledgeBaseId"`
	Name            string            `json:"name"`
	Type            DocumentType      `json:"type"`
	Status          DocumentStatus    `json:"status"`
	Content         string            `json:"content"`
	MediaID         string            `json:"mediaId,omitempty"`
	SizeBytes       int64             `json:"sizeBytes"`
	ChunkCount      int               `json:"chunkCount"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	ErrorMessage    string            `json:"errorMessage,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

func (d *Document) Normalize() {
	d.Name = strings.TrimSpace(d.Name)
	d.Content = strings.TrimSpace(d.Content)
	d.MediaID = strings.TrimSpace(d.MediaID)
	d.ErrorMessage = strings.TrimSpace(d.ErrorMessage)

	docType := strings.TrimSpace(strings.ToLower(string(d.Type)))
	if docType == "" {
		docType = string(DocumentTypeText)
	}
	d.Type = DocumentType(docType)

	if d.Status == "" {
		d.Status = DocumentStatusPending
	}

	if d.Metadata == nil {
		d.Metadata = make(map[string]string)
	}
}

func (d *Document) Validate() error {
	if d.ID == "" {
		return ErrDocumentIDRequired
	}
	if d.KnowledgeBaseID == "" {
		return ErrDocumentKnowledgeBaseRequired
	}
	if d.Name == "" {
		return ErrDocumentNameRequired
	}
	if d.Content == "" && d.MediaID == "" {
		return ErrDocumentContentRequired
	}
	if !d.Type.IsValid() {
		return ErrDocumentTypeInvalid
	}
	return nil
}

func (d *Document) MarkProcessing() {
	d.Status = DocumentStatusProcessing
	d.UpdatedAt = time.Now()
}

func (d *Document) MarkReady(chunkCount int) {
	d.Status = DocumentStatusReady
	d.ChunkCount = chunkCount
	d.UpdatedAt = time.Now()
}

func (d *Document) MarkFailed(errMsg string) {
	d.Status = DocumentStatusFailed
	d.ErrorMessage = errMsg
	d.UpdatedAt = time.Now()
}
