package rag_usecase

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/rag"
)

const (
	defaultContextWindow    = 1
	defaultOversampleFactor = 2
	defaultMinScore         = 0.3
	defaultTopK             = 5
	defaultMaxChunksPerDoc  = 3
	defaultMaxCharacters    = 10000
	defaultMinCandidates    = 10
)

type neighborKey struct {
	documentID string
	index      int
}

type queryKnowledgeBaseUseCase struct {
	embeddingService rag.EmbeddingService
	vectorRepo       rag.VectorRepository
	chunkRepo        rag.ChunkRepository
	docRepo          rag.DocumentRepository
	kbRepo           rag.KnowledgeBaseRepository
}

func NewQueryKnowledgeBaseUseCase(
	embeddingService rag.EmbeddingService,
	vectorRepo rag.VectorRepository,
	chunkRepo rag.ChunkRepository,
	docRepo rag.DocumentRepository,
	kbRepo rag.KnowledgeBaseRepository,
) rag.QueryKnowledgeBaseUseCase {
	return &queryKnowledgeBaseUseCase{
		embeddingService: embeddingService,
		vectorRepo:       vectorRepo,
		chunkRepo:        chunkRepo,
		docRepo:          docRepo,
		kbRepo:           kbRepo,
	}
}

func (uc *queryKnowledgeBaseUseCase) Execute(ctx context.Context, input rag.QueryInput) (*rag.QueryOutput, error) {
	totalStart := time.Now()

	if len(input.KnowledgeBaseIDs) == 0 {
		return &rag.QueryOutput{Results: []rag.QueryResult{}, TotalFound: 0}, nil
	}

	if input.TopK <= 0 {
		input.TopK = defaultTopK
	}
	if input.MinScore <= 0 {
		input.MinScore = defaultMinScore
	}
	contextWindow := input.ContextWindow
	if contextWindow == 0 {
		contextWindow = defaultContextWindow
	} else if contextWindow < 0 {
		contextWindow = 0
	}
	maxChunksPerDoc := input.MaxChunksPerDoc
	if maxChunksPerDoc <= 0 {
		maxChunksPerDoc = defaultMaxChunksPerDoc
	}
	maxCharacters := input.MaxCharacters
	if maxCharacters <= 0 {
		maxCharacters = defaultMaxCharacters
	}

	stepStart := time.Now()
	kbs, err := uc.kbRepo.FindByIDs(ctx, input.KnowledgeBaseIDs)
	if err != nil {
		return nil, err
	}
	if len(kbs) == 0 {
		log.Printf("[RAG Query] no KBs found for IDs: %v", input.KnowledgeBaseIDs)
		return &rag.QueryOutput{Results: []rag.QueryResult{}, TotalFound: 0}, nil
	}

	embeddingModel := kbs[0].Config.EmbeddingModel
	if embeddingModel == "" || embeddingModel == "text-embedding-3-small" {
		embeddingModel = rag.DefaultEmbeddingModel
	}
	log.Printf("[RAG Query] KBs=%d model=%s query=%q topK=%d minScore=%.2f resolve_kb=%dms",
		len(kbs), embeddingModel, input.Query, input.TopK, input.MinScore, time.Since(stepStart).Milliseconds())

	stepStart = time.Now()
	embedResult, err := uc.embeddingService.Embed(ctx, rag.EmbedInput{
		Text:  input.Query,
		Model: embeddingModel,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[RAG Timing] embed=%dms", time.Since(stepStart).Milliseconds())

	oversampledK := input.NumCandidates
	if oversampledK <= 0 {
		oversampledK = input.TopK * defaultOversampleFactor
	}
	if oversampledK < defaultMinCandidates {
		oversampledK = defaultMinCandidates
	}

	stepStart = time.Now()
	searchResult, err := uc.vectorRepo.SimilaritySearch(ctx, rag.SimilaritySearchInput{
		KnowledgeBaseIDs: input.KnowledgeBaseIDs,
		QueryEmbedding:   embedResult.Embedding,
		TopK:             oversampledK,
		MinScore:         input.MinScore,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[RAG Timing] vector_search=%dms results=%d", time.Since(stepStart).Milliseconds(), len(searchResult.Chunks))

	if len(searchResult.Chunks) == 0 {
		log.Printf("[RAG Timing] total=%dms (no results)", time.Since(totalStart).Milliseconds())
		return &rag.QueryOutput{Results: []rag.QueryResult{}, TotalFound: 0, QueryTokens: embedResult.TokenCount}, nil
	}

	deduped := deduplicateChunks(searchResult.Chunks)
	log.Printf("[RAG Query] candidates=%d after_dedup=%d", len(searchResult.Chunks), len(deduped))

	diverse := ensureDocumentDiversity(deduped, maxChunksPerDoc)

	if len(diverse) > input.TopK {
		diverse = diverse[:input.TopK]
	}

	stepStart = time.Now()

	docIDSet := make(map[string]bool, len(diverse))
	neighborReqs := make([]rag.ChunkNeighborRequest, 0, len(diverse))
	for _, chunk := range diverse {
		docIDSet[chunk.DocumentID] = true
		if contextWindow > 0 {
			minIdx := chunk.Chunk.Index - contextWindow
			if minIdx < 0 {
				minIdx = 0
			}
			neighborReqs = append(neighborReqs, rag.ChunkNeighborRequest{
				DocumentID: chunk.DocumentID,
				MinIndex:   minIdx,
				MaxIndex:   chunk.Chunk.Index + contextWindow,
			})
		}
	}

	docIDs := make([]string, 0, len(docIDSet))
	for id := range docIDSet {
		docIDs = append(docIDs, id)
	}
	docs, err := uc.docRepo.FindByIDs(ctx, docIDs)
	if err != nil {
		log.Printf("[RAG] batch doc fetch failed: %v", err)
	}
	docCache := make(map[string]*rag.Document, len(docs))
	for _, doc := range docs {
		docCache[doc.ID] = doc
	}

	neighborMap := make(map[neighborKey]*rag.Chunk)
	if len(neighborReqs) > 0 {
		allNeighbors, err := uc.chunkRepo.FindNeighborsBatch(ctx, neighborReqs)
		if err != nil {
			log.Printf("[RAG] batch neighbor fetch failed: %v", err)
		} else {
			for _, c := range allNeighbors {
				neighborMap[neighborKey{documentID: c.DocumentID, index: c.Index}] = c
			}
		}
	}

	log.Printf("[RAG Timing] batch_fetch=%dms (docs=%d neighbors=%d)", time.Since(stepStart).Milliseconds(), len(docs), len(neighborMap))

	results := make([]rag.QueryResult, 0, len(diverse))
	for _, chunk := range diverse {
		doc := docCache[chunk.DocumentID]
		docName := ""
		if doc != nil {
			docName = doc.Name
		}

		expandedContent := expandChunkFromCache(chunk, neighborMap, contextWindow)

		result := rag.QueryResult{
			Content:         expandedContent,
			Score:           chunk.Score,
			DocumentID:      chunk.DocumentID,
			DocumentName:    docName,
			KnowledgeBaseID: chunk.Chunk.KnowledgeBaseID,
			ChunkIndex:      chunk.Chunk.Index,
		}
		if input.IncludeMetadata {
			result.Metadata = chunk.Chunk.Metadata
		}
		results = append(results, result)
	}

	totalChars := 0
	for i := range results {
		if totalChars+len(results[i].Content) > maxCharacters {
			remaining := maxCharacters - totalChars
			if remaining > 0 {
				results[i].Content = results[i].Content[:remaining]
				results = results[:i+1]
			} else {
				results = results[:i]
			}
			break
		}
		totalChars += len(results[i].Content)
	}

	log.Printf("[RAG Timing] total=%dms results=%d chars=%d", time.Since(totalStart).Milliseconds(), len(results), totalChars)

	return &rag.QueryOutput{
		Results:     results,
		TotalFound:  searchResult.TotalFound,
		QueryTokens: embedResult.TokenCount,
	}, nil
}

func expandChunkFromCache(matched rag.ChunkWithScore, cache map[neighborKey]*rag.Chunk, radius int) string {
	if radius <= 0 {
		return matched.Chunk.Content
	}

	minIdx := matched.Chunk.Index - radius
	if minIdx < 0 {
		minIdx = 0
	}
	maxIdx := matched.Chunk.Index + radius

	var chunks []*rag.Chunk
	for idx := minIdx; idx <= maxIdx; idx++ {
		key := neighborKey{documentID: matched.DocumentID, index: idx}
		if c, ok := cache[key]; ok {
			chunks = append(chunks, c)
		}
	}

	if len(chunks) == 0 {
		return matched.Chunk.Content
	}

	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Index < chunks[j].Index
	})

	var sb strings.Builder
	for i, chunk := range chunks {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(strings.TrimSpace(chunk.Content))
	}

	return sb.String()
}

func deduplicateChunks(chunks []rag.ChunkWithScore) []rag.ChunkWithScore {
	if len(chunks) <= 1 {
		return chunks
	}

	type chunkKey struct {
		documentID string
		index      int
	}

	kept := make(map[chunkKey]bool)
	result := make([]rag.ChunkWithScore, 0, len(chunks))

	for _, chunk := range chunks {
		key := chunkKey{documentID: chunk.DocumentID, index: chunk.Chunk.Index}

		overlaps := false
		for delta := -1; delta <= 1; delta++ {
			neighbor := chunkKey{documentID: chunk.DocumentID, index: chunk.Chunk.Index + delta}
			if kept[neighbor] {
				overlaps = true
				break
			}
		}

		if !overlaps {
			kept[key] = true
			result = append(result, chunk)
		}
	}

	return result
}

func ensureDocumentDiversity(chunks []rag.ChunkWithScore, maxPerDoc int) []rag.ChunkWithScore {
	if maxPerDoc <= 0 {
		maxPerDoc = 3
	}

	docCounts := make(map[string]int)
	result := make([]rag.ChunkWithScore, 0, len(chunks))

	for _, chunk := range chunks {
		if docCounts[chunk.DocumentID] < maxPerDoc {
			result = append(result, chunk)
			docCounts[chunk.DocumentID]++
		}
	}

	return result
}

type queryAgentKnowledgeBaseUseCase struct {
	agentKBRepo  rag.AgentKnowledgeBaseRepository
	queryUseCase rag.QueryKnowledgeBaseUseCase
}

func NewQueryAgentKnowledgeBaseUseCase(
	agentKBRepo rag.AgentKnowledgeBaseRepository,
	queryUseCase rag.QueryKnowledgeBaseUseCase,
) rag.QueryAgentKnowledgeBaseUseCase {
	return &queryAgentKnowledgeBaseUseCase{
		agentKBRepo:  agentKBRepo,
		queryUseCase: queryUseCase,
	}
}

func (uc *queryAgentKnowledgeBaseUseCase) Execute(ctx context.Context, input rag.AgentQueryInput) (*rag.QueryOutput, error) {
	kbIDs, err := uc.agentKBRepo.FindByAgent(ctx, input.AgentID)
	if err != nil {
		return nil, err
	}

	if len(kbIDs) == 0 {
		return &rag.QueryOutput{Results: []rag.QueryResult{}, TotalFound: 0}, nil
	}

	return uc.queryUseCase.Execute(ctx, rag.QueryInput{
		KnowledgeBaseIDs: kbIDs,
		Query:            input.Query,
		TopK:             input.TopK,
		MinScore:         input.MinScore,
		IncludeMetadata:  input.IncludeMetadata,
		NumCandidates:    input.NumCandidates,
		ContextWindow:    input.ContextWindow,
		MaxChunksPerDoc:  input.MaxChunksPerDoc,
		MaxCharacters:    input.MaxCharacters,
	})
}
