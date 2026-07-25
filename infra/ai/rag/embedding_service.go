package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	rag_domain "vozko/domain/rag"
)

type embeddingCacheEntry struct {
	output   *rag_domain.EmbedOutput
	cachedAt time.Time
}

type EmbeddingService struct {
	baseURL    string
	model      string
	httpClient *http.Client
	cache      sync.Map
	cacheTTL   time.Duration
	stopCh     chan struct{}
}

func NewEmbeddingService(baseURL string, model string) *EmbeddingService {
	svc := &EmbeddingService{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		cacheTTL: 10 * time.Minute,
		stopCh:   make(chan struct{}),
	}

	go svc.warmUp(model)

	go svc.keepAliveLoop(model)
	return svc
}

func (s *EmbeddingService) warmUp(model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := s.embedTexts(ctx, []string{"warmup"}, model)
	if err != nil {
		log.Printf("[Embedding] warm-up failed for model %s: %v", model, err)
	} else {
		log.Printf("[Embedding] model %s pre-warmed", model)
	}
}

func (s *EmbeddingService) keepAliveLoop(model string) {
	ticker := time.NewTicker(4 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := s.embedTexts(ctx, []string{"keepalive"}, model)
			cancel()
			if err != nil {
				log.Printf("[Embedding] keep-alive ping failed for %s: %v", model, err)
			}
		case <-s.stopCh:
			return
		}
	}
}

func (s *EmbeddingService) Stop() {
	close(s.stopCh)
}

func (s *EmbeddingService) cacheKey(model, text string) string {
	return model + ":" + text
}

type ollamaEmbedRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	KeepAlive string   `json:"keep_alive,omitempty"`
}

type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
}

func (s *EmbeddingService) Embed(ctx context.Context, input rag_domain.EmbedInput) (*rag_domain.EmbedOutput, error) {
	model := input.Model
	if model == "" {
		model = s.model
	}

	key := s.cacheKey(model, input.Text)
	if cached, ok := s.cache.Load(key); ok {
		entry := cached.(*embeddingCacheEntry)
		if time.Since(entry.cachedAt) < s.cacheTTL {
			return entry.output, nil
		}
		s.cache.Delete(key)
	}

	result, err := s.embedTexts(ctx, []string{input.Text}, model)
	if err != nil {
		return nil, err
	}

	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned from Ollama")
	}

	embedding := make([]float32, len(result.Embeddings[0]))
	for i, v := range result.Embeddings[0] {
		embedding[i] = float32(v)
	}

	output := &rag_domain.EmbedOutput{
		Embedding:  embedding,
		TokenCount: len(input.Text) / 4,
		Model:      model,
	}

	s.cache.Store(key, &embeddingCacheEntry{output: output, cachedAt: time.Now()})

	return output, nil
}

func (s *EmbeddingService) EmbedBatch(ctx context.Context, input rag_domain.EmbedBatchInput) (*rag_domain.EmbedBatchOutput, error) {
	model := input.Model
	if model == "" {
		model = s.model
	}

	if len(input.Texts) == 0 {
		return &rag_domain.EmbedBatchOutput{
			Embeddings:  [][]float32{},
			TokenCounts: []int{},
			Model:       model,
			TotalTokens: 0,
		}, nil
	}

	result, err := s.embedTexts(ctx, input.Texts, model)
	if err != nil {
		return nil, err
	}

	embeddings := make([][]float32, len(result.Embeddings))
	tokenCounts := make([]int, len(input.Texts))
	totalTokens := 0

	for i, emb := range result.Embeddings {
		embedding := make([]float32, len(emb))
		for j, v := range emb {
			embedding[j] = float32(v)
		}
		embeddings[i] = embedding
		tokenCounts[i] = len(input.Texts[i]) / 4
		totalTokens += tokenCounts[i]
	}

	return &rag_domain.EmbedBatchOutput{
		Embeddings:  embeddings,
		TokenCounts: tokenCounts,
		Model:       model,
		TotalTokens: totalTokens,
	}, nil
}

func (s *EmbeddingService) GetDimensions(model string) int {
	switch model {
	case "bge-m3":
		return 1024
	case "nomic-embed-text":
		return 768
	case "mxbai-embed-large":
		return 1024
	case "all-minilm", "all-MiniLM-L6-v2":
		return 384
	default:
		return rag_domain.DefaultEmbeddingDim
	}
}

func (s *EmbeddingService) SupportedModels() []string {
	return []string{
		"bge-m3",
		"nomic-embed-text",
		"mxbai-embed-large",
		"all-minilm",
	}
}

func (s *EmbeddingService) embedTexts(ctx context.Context, texts []string, model string) (*ollamaEmbedResponse, error) {
	requestBody := ollamaEmbedRequest{
		Model:     model,
		Input:     texts,
		KeepAlive: "30m",
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embed request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embed", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create embed request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute embed request to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}

	return &result, nil
}
