package rag

import "context"

type EmbeddingService interface {
	Embed(ctx context.Context, input EmbedInput) (*EmbedOutput, error)
	EmbedBatch(ctx context.Context, input EmbedBatchInput) (*EmbedBatchOutput, error)
	GetDimensions(model string) int
	SupportedModels() []string
}

type EmbedInput struct {
	Text  string
	Model string
}

type EmbedOutput struct {
	Embedding  []float32
	TokenCount int
	Model      string
}

type EmbedBatchInput struct {
	Texts []string
	Model string
}

type EmbedBatchOutput struct {
	Embeddings  [][]float32
	TokenCounts []int
	Model       string
	TotalTokens int
}

type TextChunker interface {
	Chunk(text string, config ChunkingConfig) []TextChunk
}

type ChunkingConfig struct {
	Strategy ChunkingStrategy
	Size     int
	Overlap  int
}

type TextChunk struct {
	Content     string
	Index       int
	StartOffset int
	EndOffset   int
}
