package rag

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"vozko/domain/rag"
)

const (
	defaultBatchSize = 10

	defaultChunkSize = 800

	defaultChunkOverlap = 100

	// maxChunkRunes hard-caps every chunk so it always fits the embedding model's context.
	maxChunkRunes = 2000
)

type documentProcessor struct {
	docRepo          rag.DocumentRepository
	kbRepo           rag.KnowledgeBaseRepository
	chunkRepo        rag.ChunkRepository
	embeddingService rag.EmbeddingService
	textChunker      rag.TextChunker
	textExtractor    rag.TextExtractor
	batchSize        int
}

func NewDocumentProcessor(
	docRepo rag.DocumentRepository,
	kbRepo rag.KnowledgeBaseRepository,
	chunkRepo rag.ChunkRepository,
	embeddingService rag.EmbeddingService,
	textChunker rag.TextChunker,
	textExtractor rag.TextExtractor,
) rag.DocumentProcessor {
	return &documentProcessor{
		docRepo:          docRepo,
		kbRepo:           kbRepo,
		chunkRepo:        chunkRepo,
		embeddingService: embeddingService,
		textChunker:      textChunker,
		textExtractor:    textExtractor,
		batchSize:        defaultBatchSize,
	}
}

func (p *documentProcessor) Process(ctx context.Context, doc *rag.Document) error {
	log.Printf("[RAG] processing document %s (%s, type=%s), %d bytes", doc.ID, doc.Name, doc.Type, doc.SizeBytes)

	if err := p.docRepo.UpdateStatus(ctx, doc.ID, rag.DocumentStatusProcessing, ""); err != nil {
		return p.fail(ctx, doc.ID, "status update failed: %v", err)
	}

	content, err := p.resolveContent(doc)
	if err != nil {
		return p.fail(ctx, doc.ID, "text extraction failed: %v", err)
	}

	if strings.TrimSpace(content) == "" {
		return p.fail(ctx, doc.ID, "no extractable text found in document")
	}

	log.Printf("[RAG] document %s, extracted %d characters of text", doc.ID, len(content))

	textChunks := ChunkDocument(p.textChunker, content, doc.Name)

	if len(textChunks) == 0 {
		log.Printf("[RAG] document %s produced 0 chunks, marking ready", doc.ID)
		return p.markReady(ctx, doc, 0)
	}

	log.Printf("[RAG] document %s → %d chunks, embedding in batches of %d",
		doc.ID, len(textChunks), p.batchSize)

	allChunks := make([]*rag.Chunk, 0, len(textChunks))

	for i := 0; i < len(textChunks); i += p.batchSize {
		end := i + p.batchSize
		if end > len(textChunks) {
			end = len(textChunks)
		}
		batch := textChunks[i:end]

		texts := make([]string, len(batch))
		for j, tc := range batch {
			texts[j] = tc.Content
		}

		embedResult, err := p.embeddingService.EmbedBatch(ctx, rag.EmbedBatchInput{Texts: texts})
		if err != nil {
			return p.fail(ctx, doc.ID, "embedding batch %d–%d failed: %v", i, end, err)
		}

		for j, tc := range batch {
			allChunks = append(allChunks, &rag.Chunk{
				ID:              uuid.New().String(),
				DocumentID:      doc.ID,
				KnowledgeBaseID: doc.KnowledgeBaseID,
				Content:         tc.Content,
				Embedding:       embedResult.Embeddings[j],
				Index:           tc.Index,
				StartOffset:     tc.StartOffset,
				EndOffset:       tc.EndOffset,
				Metadata:        doc.Metadata,
				TokenCount:      estimateTokens(tc.Content),
				CreatedAt:       time.Now(),
			})
		}

		log.Printf("[RAG] document %s, embedded batch %d/%d", doc.ID, end, len(textChunks))
	}

	if err := p.chunkRepo.CreateBatch(ctx, allChunks); err != nil {
		return p.fail(ctx, doc.ID, "chunk storage failed: %v", err)
	}

	return p.markReady(ctx, doc, len(allChunks))
}

func (p *documentProcessor) markReady(ctx context.Context, doc *rag.Document, chunkCount int) error {
	doc.ChunkCount = chunkCount
	doc.Status = rag.DocumentStatusReady
	doc.ErrorMessage = ""
	doc.UpdatedAt = time.Now()

	if err := p.docRepo.Update(ctx, doc); err != nil {
		return fmt.Errorf("failed to update document %s: %w", doc.ID, err)
	}

	if chunkCount > 0 {
		_ = p.kbRepo.IncrementChunkCount(ctx, doc.KnowledgeBaseID, chunkCount)
	}

	log.Printf("[RAG] document %s ready, %d chunks stored", doc.ID, chunkCount)
	return nil
}

func (p *documentProcessor) resolveContent(doc *rag.Document) (string, error) {
	return ResolveDocumentContent(p.textExtractor, doc)
}

// ResolveDocumentContent turns a stored document into extractable text: base64-encoded
// uploads are decoded and run through the extractor; PDF/DOCX stored raw are extracted
// (falling back to raw on failure); anything else is returned as-is. Exported so the
// re-index tool ingests documents through the exact same path as the live processor.
func ResolveDocumentContent(extractor rag.TextExtractor, doc *rag.Document) (string, error) {
	content := doc.Content

	if doc.Metadata != nil && doc.Metadata["encoding"] == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 content: %w", err)
		}
		log.Printf("[RAG] document %s, decoded %d bytes from base64", doc.ID, len(decoded))

		extracted, err := extractor.Extract(decoded, doc.Name)
		if err != nil {
			return "", fmt.Errorf("text extraction failed for %s: %w", doc.Name, err)
		}
		return extracted, nil
	}

	switch doc.Type {
	case rag.DocumentTypePDF, rag.DocumentTypeDocx:

		extracted, err := extractor.Extract([]byte(content), doc.Name)
		if err != nil {

			log.Printf("[RAG] document %s, extraction failed, using raw content: %v", doc.ID, err)
			return content, nil
		}
		return extracted, nil
	default:

		return content, nil
	}
}

func (p *documentProcessor) fail(ctx context.Context, docID string, format string, args ...interface{}) error {
	errMsg := fmt.Sprintf(format, args...)
	log.Printf("[RAG] document %s FAILED: %s", docID, errMsg)

	if updateErr := p.docRepo.UpdateStatus(ctx, docID, rag.DocumentStatusFailed, errMsg); updateErr != nil {
		log.Printf("[RAG] could not update status for document %s: %v", docID, updateErr)
	}

	return fmt.Errorf("%s", errMsg)
}

// ChunkDocument is the shared entry point for the processor and tooling: it selects the
// chunking strategy by file type and normalizes the result (UTF-8, size cap, reindex).
func ChunkDocument(chunker rag.TextChunker, content, name string) []rag.TextChunk {
	var raw []rag.TextChunk
	if isTabular(name) {
		raw = splitRecords(content)
	} else {
		raw = chunker.Chunk(content, rag.ChunkingConfig{
			Strategy: rag.ChunkingStrategySentence,
			Size:     defaultChunkSize,
			Overlap:  defaultChunkOverlap,
		})
	}
	return normalizeChunks(raw)
}

func isTabular(name string) bool {
	switch strings.ToLower(fileExtension(name)) {
	case ".xlsx", ".xls", ".csv":
		return true
	default:
		return false
	}
}

// splitRecords turns the extractor's blank-line-separated records into one chunk each.
func splitRecords(content string) []rag.TextChunk {
	parts := strings.Split(content, "\n\n")
	chunks := make([]rag.TextChunk, 0, len(parts))
	offset := 0
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			chunks = append(chunks, rag.TextChunk{
				Content:     trimmed,
				Index:       len(chunks),
				StartOffset: offset,
				EndOffset:   offset + len(p),
			})
		}
		offset += len(p) + 2
	}
	return chunks
}

// normalizeChunks guarantees every chunk is valid UTF-8 and within maxChunkRunes
// (splitting oversized ones on whitespace) and reassigns contiguous indexes.
func normalizeChunks(chunks []rag.TextChunk) []rag.TextChunk {
	out := make([]rag.TextChunk, 0, len(chunks))
	for _, c := range chunks {
		content := sanitizeUTF8(c.Content)
		for _, piece := range splitToMaxRunes(content, maxChunkRunes) {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			out = append(out, rag.TextChunk{
				Content:     piece,
				Index:       len(out),
				StartOffset: c.StartOffset,
				EndOffset:   c.EndOffset,
			})
		}
	}
	return out
}

// splitToMaxRunes splits s into pieces of at most maxRunes runes, preferring the last
// whitespace before the limit so words are not cut in half.
func splitToMaxRunes(s string, maxRunes int) []string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return []string{s}
	}

	var pieces []string
	for len(runes) > 0 {
		if len(runes) <= maxRunes {
			pieces = append(pieces, string(runes))
			break
		}
		cut := maxRunes
		for cut > maxRunes/2 && !unicode.IsSpace(runes[cut-1]) {
			cut--
		}
		if cut <= maxRunes/2 {
			cut = maxRunes
		}
		pieces = append(pieces, string(runes[:cut]))
		runes = runes[cut:]
	}
	return pieces
}

func estimateTokens(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		n = 1
	}
	return n
}
