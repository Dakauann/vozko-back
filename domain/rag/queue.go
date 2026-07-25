package rag

const (
	DocumentProcessingExchange = "rag_document_processing_exchange"

	DocumentProcessingTopic = "rag_document_processing"
)

type DocumentProcessingMessage struct {
	DocumentID      string `json:"document_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Attempt         int    `json:"attempt"`
}

const (
	MaxProcessingAttempts = 3
)
