package rag

import "context"

type RAGService interface {
	Query(ctx context.Context, input QueryInput) (*QueryOutput, error)
	QueryForAgent(ctx context.Context, input AgentQueryInput) (*QueryOutput, error)
}

type QueryInput struct {
	KnowledgeBaseIDs []string
	Query            string
	TopK             int
	MinScore         float32
	IncludeMetadata  bool

	NumCandidates   int
	ContextWindow   int
	MaxChunksPerDoc int
	MaxCharacters   int
}

type AgentQueryInput struct {
	AgentID         string
	Query           string
	TopK            int
	MinScore        float32
	IncludeMetadata bool

	NumCandidates   int
	ContextWindow   int
	MaxChunksPerDoc int
	MaxCharacters   int
}

type QueryOutput struct {
	Results     []QueryResult `json:"results"`
	TotalFound  int           `json:"totalFound"`
	QueryTokens int           `json:"queryTokens"`
}

type QueryResult struct {
	Content         string            `json:"content"`
	Score           float32           `json:"score"`
	DocumentID      string            `json:"documentId"`
	DocumentName    string            `json:"documentName"`
	KnowledgeBaseID string            `json:"knowledgeBaseId"`
	ChunkIndex      int               `json:"chunkIndex"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type DocumentProcessor interface {
	Process(ctx context.Context, doc *Document) error
}

type TextExtractor interface {
	Extract(data []byte, filename string) (string, error)
}

type PublishDocumentProcessingUseCase interface {
	Execute(ctx context.Context, documentID string) error
}

type ConsumeDocumentProcessingUseCase interface {
	Start() error
}

type CreateKnowledgeBaseInput struct {
	WorkspaceID string
	Name        string
	Description string
	Config      KnowledgeBaseConfig
}

type CreateDocumentInput struct {
	KnowledgeBaseID string
	Name            string
	Type            DocumentType
	Content         string
	MediaID         string
	Metadata        map[string]string
}

type UpdateKnowledgeBaseInput struct {
	Name        *string
	Description *string
	Config      *KnowledgeBaseConfig
}

type LinkAgentKnowledgeBasesInput struct {
	AgentID          string
	KnowledgeBaseIDs []string
}

type CreateKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, input CreateKnowledgeBaseInput) (*KnowledgeBase, error)
}

type UpdateKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, id string, input UpdateKnowledgeBaseInput) (*KnowledgeBase, error)
}

type DeleteKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, id string) error
}

type GetKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, id string) (*KnowledgeBase, error)
}

type ListKnowledgeBasesUseCase interface {
	Execute(ctx context.Context, workspaceID string, departmentID *string, page int, pageSize int) (*KnowledgeBaseListOutput, error)
}

type KnowledgeBaseListOutput struct {
	Items      []*KnowledgeBase `json:"items"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	TotalPages int              `json:"totalPages"`
}

type CreateDocumentUseCase interface {
	Execute(ctx context.Context, input CreateDocumentInput) (*Document, error)
}

type DeleteDocumentUseCase interface {
	Execute(ctx context.Context, id string) error
}

type GetDocumentUseCase interface {
	Execute(ctx context.Context, id string) (*Document, error)
}

type ListDocumentsUseCase interface {
	Execute(ctx context.Context, knowledgeBaseID string, page int, pageSize int) (*DocumentListOutput, error)
}

type DocumentListOutput struct {
	Items      []*Document `json:"items"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"pageSize"`
	TotalPages int         `json:"totalPages"`
}

type LinkAgentKnowledgeBasesUseCase interface {
	Execute(ctx context.Context, input LinkAgentKnowledgeBasesInput) error
}

type GetAgentKnowledgeBasesUseCase interface {
	Execute(ctx context.Context, agentID string) ([]*KnowledgeBase, error)
}

type QueryKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, input QueryInput) (*QueryOutput, error)
}

type QueryAgentKnowledgeBaseUseCase interface {
	Execute(ctx context.Context, input AgentQueryInput) (*QueryOutput, error)
}
