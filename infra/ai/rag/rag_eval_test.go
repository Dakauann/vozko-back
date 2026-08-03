package rag

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	ragdomain "vozko/domain/rag"
)

// This is an end-to-end retrieval-quality harness for the RAG ingestion pipeline. It
// runs the REAL extractor + chunker + embedding model over a corpus of files and
// measures retrieval metrics (hit@k, MRR) against a manifest of expected answers, so a
// change to extraction/chunking can be proven to help or hurt before shipping.
//
// It is skipped unless RAG_EVAL=1 (it needs a running Ollama with bge-m3). Drive it with:
//
//	RAG_EVAL=1 RAG_EVAL_DIR=<corpus dir> OLLAMA_URL=http://localhost:11434 \
//	  go test ./infra/ai/rag/ -run TestRAGRetrievalEval -v -timeout 30m
//
// The corpus dir must contain manifest.json:
//
//	[{"file":"cursos.xlsx","min_hit3":0.9,"queries":[
//	    {"q":"quanto ganha administração","all":["ADMINISTRAÇÃO","4,020.29"]}]}]

type evalQuery struct {
	Q   string   `json:"q"`
	All []string `json:"all"` // a top-k chunk must contain all of these (case-insensitive) to be a hit
}

type evalFile struct {
	File    string      `json:"file"`
	MinHit3 float64     `json:"min_hit3"` // assertion threshold for hit@3 (0 = no assert)
	MinMRR  float64     `json:"min_mrr"`
	Queries []evalQuery `json:"queries"`
}

func TestRAGRetrievalEval(t *testing.T) {
	if os.Getenv("RAG_EVAL") != "1" {
		t.Skip("set RAG_EVAL=1 (needs Ollama + bge-m3) to run the retrieval-quality harness")
	}
	dir := os.Getenv("RAG_EVAL_DIR")
	if dir == "" {
		t.Fatal("RAG_EVAL_DIR must point to a corpus directory with manifest.json")
	}
	ollama := os.Getenv("OLLAMA_URL")
	if ollama == "" {
		ollama = "http://localhost:11434"
	}

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest []evalFile
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	extractor := NewTextExtractor()
	chunker := NewTextChunker()
	embedder := NewEmbeddingService(ollama, ragdomain.DefaultEmbeddingModel)
	defer embedder.Stop()
	ctx := context.Background()

	var gTotal, gHit1, gHit3, gHit5 int
	var gMRR float64

	for _, ef := range manifest {
		t.Run(ef.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, ef.File))
			if err != nil {
				t.Fatalf("read %s: %v", ef.File, err)
			}

			text, err := extractor.Extract(data, ef.File)
			if err != nil {
				t.Fatalf("extract %s: %v", ef.File, err)
			}
			chunks := ChunkDocument(chunker, text, ef.File)
			if len(chunks) == 0 {
				t.Fatalf("%s produced 0 chunks", ef.File)
			}

			chunkVecs := embedAll(ctx, t, embedder, chunkTexts(chunks))

			var hit1, hit3, hit5, total int
			var mrr float64
			for _, q := range ef.Queries {
				qv := embedOne(ctx, t, embedder, q.Q)
				order := topKByCosine(qv, chunkVecs, 5)
				rank := firstHitRank(order, chunks, q.All)
				total++
				if rank == 1 {
					hit1++
				}
				if rank >= 1 && rank <= 3 {
					hit3++
				}
				if rank >= 1 && rank <= 5 {
					hit5++
				}
				if rank >= 1 {
					mrr += 1.0 / float64(rank)
				} else {
					t.Logf("  MISS q=%q expected(all)=%v", q.Q, q.All)
				}
			}

			h1 := ratio(hit1, total)
			h3 := ratio(hit3, total)
			h5 := ratio(hit5, total)
			m := mrr / float64(max1(total))
			t.Logf("%-42s chunks=%3d  n=%3d  hit@1=%.2f  hit@3=%.2f  hit@5=%.2f  MRR=%.3f",
				ef.File, len(chunks), total, h1, h3, h5, m)

			gTotal += total
			gHit1 += hit1
			gHit3 += hit3
			gHit5 += hit5
			gMRR += mrr

			if ef.MinHit3 > 0 && h3 < ef.MinHit3 {
				t.Errorf("%s hit@3=%.2f below threshold %.2f", ef.File, h3, ef.MinHit3)
			}
			if ef.MinMRR > 0 && m < ef.MinMRR {
				t.Errorf("%s MRR=%.3f below threshold %.3f", ef.File, m, ef.MinMRR)
			}
		})
	}

	t.Logf("==== OVERALL n=%d hit@1=%.3f hit@3=%.3f hit@5=%.3f MRR=%.3f ====",
		gTotal, ratio(gHit1, gTotal), ratio(gHit3, gTotal), ratio(gHit5, gTotal), gMRR/float64(max1(gTotal)))
}

// TestRAGIngestInvariants proves the ingestion-hardening fixes on the real documents
// that previously failed: every chunk is valid UTF-8 (no stray 0xa7 reaching Postgres)
// and small enough that the embedder never rejects it with "input length exceeds the
// context length". It embeds every chunk to confirm end to end.
func TestRAGIngestInvariants(t *testing.T) {
	if os.Getenv("RAG_EVAL") != "1" {
		t.Skip("set RAG_EVAL=1 (needs Ollama + bge-m3) to run ingestion invariants")
	}
	dir := os.Getenv("RAG_EVAL_DIR")
	if dir == "" {
		t.Fatal("RAG_EVAL_DIR must point to the corpus directory")
	}
	ollama := os.Getenv("OLLAMA_URL")
	if ollama == "" {
		ollama = "http://localhost:11434"
	}

	// Files that failed in production: 0xa7 UTF-8 byte, and a chunk over the embed limit.
	files := []string{"failed_terapias.pdf", "failed_engenharia.pdf", "guia_percurso_admin.pdf"}
	if extra := os.Getenv("RAG_INGEST_FILES"); extra != "" {
		files = strings.Split(extra, ",")
	}

	extractor := NewTextExtractor()
	chunker := NewTextChunker()
	embedder := NewEmbeddingService(ollama, ragdomain.DefaultEmbeddingModel)
	defer embedder.Stop()
	ctx := context.Background()

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				t.Skipf("missing %s: %v", f, err)
			}
			text, err := extractor.Extract(data, f)
			if err != nil {
				t.Fatalf("extract failed: %v", err)
			}
			chunks := ChunkDocument(chunker, text, f)
			if len(chunks) == 0 {
				t.Fatal("produced 0 chunks")
			}
			maxRunes := 0
			for i, c := range chunks {
				if !utf8.ValidString(c.Content) {
					t.Fatalf("chunk %d is not valid UTF-8 (would break Postgres insert)", i)
				}
				if n := len([]rune(c.Content)); n > maxChunkRunes {
					t.Fatalf("chunk %d has %d runes, exceeds cap %d (would break embedding)", i, n, maxChunkRunes)
				} else if n > maxRunes {
					maxRunes = n
				}
			}
			// Embed every chunk: this is the exact step that used to 400.
			_ = embedAll(ctx, t, embedder, chunkTexts(chunks))
			t.Logf("%-32s chunks=%d maxRunes=%d, all valid UTF-8, all embedded OK", f, len(chunks), maxRunes)
		})
	}
}

func chunkTexts(chunks []ragdomain.TextChunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Content
	}
	return out
}

func embedAll(ctx context.Context, t *testing.T, e *EmbeddingService, texts []string) [][]float32 {
	t.Helper()
	out := make([][]float32, 0, len(texts))
	const batch = 16
	for i := 0; i < len(texts); i += batch {
		end := i + batch
		if end > len(texts) {
			end = len(texts)
		}
		cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		res, err := e.EmbedBatch(cctx, ragdomain.EmbedBatchInput{Texts: texts[i:end]})
		cancel()
		if err != nil {
			t.Fatalf("embed batch %d-%d: %v", i, end, err)
		}
		out = append(out, res.Embeddings...)
	}
	return out
}

func embedOne(ctx context.Context, t *testing.T, e *EmbeddingService, text string) []float32 {
	t.Helper()
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := e.Embed(cctx, ragdomain.EmbedInput{Text: text})
	if err != nil {
		t.Fatalf("embed query %q: %v", text, err)
	}
	return res.Embedding
}

func topKByCosine(q []float32, docs [][]float32, k int) []int {
	type sc struct {
		i int
		s float64
	}
	scores := make([]sc, len(docs))
	for i, d := range docs {
		scores[i] = sc{i, cosine(q, d)}
	}
	sort.Slice(scores, func(a, b int) bool { return scores[a].s > scores[b].s })
	if k > len(scores) {
		k = len(scores)
	}
	out := make([]int, k)
	for i := 0; i < k; i++ {
		out[i] = scores[i].i
	}
	return out
}

// firstHitRank returns the 1-based rank of the first chunk (in retrieval order) whose
// content contains every expected substring, or 0 if none of the top-k match.
func firstHitRank(order []int, chunks []ragdomain.TextChunk, all []string) int {
	for rank, idx := range order {
		if containsAll(chunks[idx].Content, all) {
			return rank + 1
		}
	}
	return 0
}

func containsAll(content string, subs []string) bool {
	lc := strings.ToLower(content)
	for _, s := range subs {
		if !strings.Contains(lc, strings.ToLower(strings.TrimSpace(s))) {
			return false
		}
	}
	return true
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
