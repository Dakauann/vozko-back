package rag_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/rag"
)

// ContextInput selects which knowledge to retrieve for a single agent turn.
// Retrieval precedence matches the workflow executors: an agent with RAG enabled
// wins; otherwise KnowledgeBaseIDs (if any) are queried directly.
type ContextInput struct {
	Agent            *agent.Agent
	KnowledgeBaseIDs []string
	Query            string
}

// BuildContext retrieves knowledge for Query and renders it (grounding rules +
// chunks) as a system-prompt suffix, or "" when nothing applies or matches. This
// is the single source of RAG context for every channel (chat, voice, simulator).
func BuildContext(ctx context.Context, ragService rag.RAGService, in ContextInput) string {
	if ragService == nil || strings.TrimSpace(in.Query) == "" {
		return ""
	}

	var results []rag.QueryResult

	switch {
	case in.Agent != nil && in.Agent.IsRAGEnabled():
		cfg := in.Agent.RAGConfig.WithDefaults()
		out, err := ragService.QueryForAgent(ctx, rag.AgentQueryInput{
			AgentID:         in.Agent.ID,
			Query:           in.Query,
			TopK:            cfg.MaxChunks,
			MinScore:        float32(cfg.MinScore),
			IncludeMetadata: true,
			NumCandidates:   cfg.NumCandidates,
			ContextWindow:   cfg.ContextWindow,
			MaxChunksPerDoc: cfg.MaxChunksPerDoc,
			MaxCharacters:   cfg.MaxCharacters,
		})
		if err != nil {
			log.Printf("[RAG] failed to query knowledge base for agent %s: %v", in.Agent.ID, err)
			return ""
		}
		results = out.Results
		if len(results) > 0 {
			log.Printf("[RAG] found %d results for agent %s query: %q", len(results), in.Agent.ID, in.Query)
		}

	case len(in.KnowledgeBaseIDs) > 0:
		out, err := ragService.Query(ctx, rag.QueryInput{
			KnowledgeBaseIDs: in.KnowledgeBaseIDs,
			Query:            in.Query,
			TopK:             10,
			MinScore:         0.3,
			IncludeMetadata:  true,
		})
		if err != nil {
			log.Printf("[RAG] failed to query knowledge bases %v: %v", in.KnowledgeBaseIDs, err)
			return ""
		}
		results = out.Results
		if len(results) > 0 {
			log.Printf("[RAG] found %d results for KBs %v query: %q", len(results), in.KnowledgeBaseIDs, in.Query)
		}

	default:
		return ""
	}

	return FormatRAGContext(results)
}

// BuildRAGContext is the agent-scoped shortcut kept for existing callers.
func BuildRAGContext(ctx context.Context, ragService rag.RAGService, ag *agent.Agent, userMessage string) string {
	return BuildContext(ctx, ragService, ContextInput{Agent: ag, Query: userMessage})
}

// ContextHeader opens the knowledge-base block. Exported so surfaces that need
// to know whether a prompt carries RAG grounding (the agent simulator's debug
// view) test against the one real header instead of a copied literal.
const ContextHeader = "# Contexto adicional (base de conhecimento)"

// FormatRAGContext renders retrieved chunks as fenced reference context. Empty
// results render as "". The framing is deliberately soft: the chunks are supporting
// material, not a behavioral override. It guards against fabricating specific data,
// but does NOT force a canned refusal or suppress the agent's own persona when the
// chunks do not cover the question. This is the single source of the grounding block.
func FormatRAGContext(results []rag.QueryResult) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n" + ContextHeader + "\n")
	sb.WriteString("As informações abaixo foram recuperadas da base de conhecimento e podem ser relevantes. Trate-as como material de referência:\n")
	sb.WriteString("- Baseie-se nelas para dados específicos (valores, preços, datas, nomes, cursos, disponibilidade) e não invente esse tipo de dado se não estiver presente aqui.\n")
	sb.WriteString("- Se estes trechos não cobrirem a pergunta, responda normalmente seguindo as suas próprias instruções e persona. Não mencione esta base de conhecimento nem diga que \"não tem a informação\", a menos que suas instruções peçam.\n\n")

	for i, r := range results {
		if r.DocumentName != "" {
			sb.WriteString(fmt.Sprintf("[%d] (Fonte: %s)\n%s\n\n", i+1, r.DocumentName, r.Content))
		} else {
			sb.WriteString(fmt.Sprintf("[%d]\n%s\n\n", i+1, r.Content))
		}
	}

	sb.WriteString("# Fim do contexto adicional\n\n")
	return sb.String()
}
