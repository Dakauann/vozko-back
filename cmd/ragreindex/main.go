// Command ragreindex re-ingests already-stored RAG documents through the current
// extraction + chunking + embedding pipeline and atomically replaces their chunks.
// Use it after an ingestion change (e.g. structure-aware table chunking) to fix
// existing documents without re-uploading. It shares the exact code path the live
// document processor uses (ResolveDocumentContent + ChunkDocument).
//
//	# reindex specific documents
//	go run ./cmd/ragreindex -docs id1,id2
//	# reindex every spreadsheet, or a whole KB, or every failed document
//	go run ./cmd/ragreindex -ext xlsx
//	go run ./cmd/ragreindex -kb <kb-id>
//	go run ./cmd/ragreindex -status failed
//	# preview only (no writes)
//	go run ./cmd/ragreindex -ext xlsx -dry
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	ragdomain "vozko/domain/rag"
	raginfra "vozko/infra/ai/rag"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const embedBatch = 16

var errEmpty = errors.New("document produced 0 chunks")

func main() {
	docs := flag.String("docs", "", "comma-separated document ids to reindex")
	kb := flag.String("kb", "", "reindex every document in this knowledge base")
	ext := flag.String("ext", "", "reindex every document whose name ends with this extension (e.g. xlsx)")
	status := flag.String("status", "", "reindex every document with this status (e.g. failed)")
	dry := flag.Bool("dry", false, "extract+chunk only, print stats, do not write")
	flag.Parse()

	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	// Silence per-statement SQL logging; bulk chunk inserts would flood the output.
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Error)})

	q := db.Model(&schema.RAGDocument{})
	switch {
	case *docs != "":
		q = q.Where("id IN ?", strings.Split(*docs, ","))
	case *kb != "":
		q = q.Where("knowledge_base_id = ?", *kb)
	case *ext != "":
		q = q.Where("lower(name) LIKE ?", "%."+strings.ToLower(strings.TrimPrefix(*ext, ".")))
	case *status != "":
		q = q.Where("status = ?", *status)
	default:
		log.Fatal("specify one of -docs / -kb / -ext / -status")
	}

	var rows []schema.RAGDocument
	if err := q.Find(&rows).Error; err != nil {
		log.Fatalf("query documents: %v", err)
	}
	log.Printf("reindexing %d document(s) (dry=%v)", len(rows), *dry)

	ollama := os.Getenv("OLLAMA_URL")
	if ollama == "" {
		ollama = "http://localhost:11434"
	}
	extractor := raginfra.NewTextExtractor()
	chunker := raginfra.NewTextChunker()
	embedder := raginfra.NewEmbeddingService(ollama, ragdomain.DefaultEmbeddingModel)
	defer embedder.Stop()
	ctx := context.Background()

	var okCount, failCount int
	for i := range rows {
		r := rows[i]
		if err := reindexOne(ctx, db, extractor, chunker, embedder, &r, *dry); err != nil {
			failCount++
			log.Printf("  FAIL %s (%s): %v", r.ID, r.Name, err)
			continue
		}
		okCount++
	}
	log.Printf("done: %d ok, %d failed", okCount, failCount)
}

func reindexOne(ctx context.Context, db *gorm.DB, extractor ragdomain.TextExtractor, chunker ragdomain.TextChunker, embedder *raginfra.EmbeddingService, row *schema.RAGDocument, dry bool) error {
	meta := map[string]string{}
	if strings.TrimSpace(row.Metadata) != "" {
		_ = json.Unmarshal([]byte(row.Metadata), &meta)
	}
	doc := &ragdomain.Document{
		ID:              row.ID,
		KnowledgeBaseID: row.KnowledgeBaseID,
		Name:            row.Name,
		Type:            ragdomain.DocumentType(row.Type),
		Content:         row.Content,
		Metadata:        meta,
	}

	text, err := raginfra.ResolveDocumentContent(extractor, doc)
	if err != nil {
		return err
	}
	chunks := raginfra.ChunkDocument(chunker, text, doc.Name)
	if len(chunks) == 0 {
		return errEmpty
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vecs := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += embedBatch {
		end := i + embedBatch
		if end > len(texts) {
			end = len(texts)
		}
		out, err := embedder.EmbedBatch(ctx, ragdomain.EmbedBatchInput{Texts: texts[i:end]})
		if err != nil {
			return err
		}
		vecs = append(vecs, out.Embeddings...)
	}

	log.Printf("  %s (%s): %d chunks", row.ID, row.Name, len(chunks))
	if dry {
		return nil
	}

	newChunks := make([]schema.RAGChunk, len(chunks))
	for i, c := range chunks {
		newChunks[i] = schema.RAGChunk{
			ID:              uuid.New().String(),
			DocumentID:      row.ID,
			KnowledgeBaseID: row.KnowledgeBaseID,
			Content:         c.Content,
			Embedding:       pgvector.NewVector(vecs[i]),
			Index:           c.Index,
			StartOffset:     c.StartOffset,
			EndOffset:       c.EndOffset,
			Metadata:        "{}",
			TokenCount:      len(c.Content) / 4,
			CreatedAt:       time.Now(),
		}
	}

	delta := len(newChunks) - row.ChunkCount
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", row.ID).Delete(&schema.RAGChunk{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(&newChunks, 100).Error; err != nil {
			return err
		}
		if err := tx.Model(&schema.RAGDocument{}).Where("id = ?", row.ID).
			Updates(map[string]any{"chunk_count": len(newChunks), "status": "ready", "error_message": "", "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&schema.KnowledgeBase{}).Where("id = ?", row.KnowledgeBaseID).
			UpdateColumn("chunk_count", gorm.Expr("GREATEST(0, chunk_count + ?)", delta)).Error
	})
}
