package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/domain/agent"
	"vozko/domain/rag"
	"vozko/domain/tools"
	"vozko/usecases/agentctx"
)

const SearchKnowledgeBaseToolName = "search_knowledge_base"

type searchKnowledgeBaseTool struct {
	rag rag.RAGService
}

// NewSearchKnowledgeBaseToolUseCase lets the agent actively query its own
// knowledge bases mid-turn. It complements the passive per-turn injection in
// agentturn (which uses the raw user message as query): here the model
// formulates and refines the query itself.
func NewSearchKnowledgeBaseToolUseCase(ragService rag.RAGService) tools.Handler {
	if ragService == nil {
		return nil
	}
	return &searchKnowledgeBaseTool{rag: ragService}
}

func (t *searchKnowledgeBaseTool) Definition() tools.Definition {
	return tools.Definition{
		Name:               SearchKnowledgeBaseToolName,
		DisplayName:        "Buscar na base de conhecimento",
		DisplayDescription: "Permite ao agente pesquisar ativamente na base de conhecimento durante a conversa, reformulando a busca quando necessário.",
		Description: `Busca na base de conhecimento do agente e retorna os trechos mais relevantes usando busca semântica (por significado) — a consulta não precisa repetir as palavras exatas dos documentos. Retorna trechos ordenados por relevância, numerados e com a fonte de cada um; não retorna documentos inteiros e não cobre assuntos fora da base (conhecimento geral, notícias).

QUANDO USAR:
- O cliente perguntou sobre produtos, preços, políticas, prazos, condições ou qualquer dado que deva estar nos documentos da empresa → busque ANTES de responder, não confie na memória
- O contexto já fornecido não cobre a pergunta ou está incompleto

COMO FORMULAR A CONSULTA:
- Use os termos-chave do assunto em uma frase específica (ex.: 'valor da mensalidade do plano empresarial'), nunca palavras soltas e genéricas
- A mensagem do cliente raramente é a melhor consulta: extraia O QUE ele quer saber e busque por isso
- Para fatos diferentes, faça uma busca separada e focada para cada fato

SE NÃO ENCONTRAR (obrigatório antes de desistir):
1. Reformule com sinônimos e termos que a empresa usaria nos documentos (ex.: 'mensalidade' → 'preço do plano', 'valor')
2. Tente uma versão mais ampla ou mais específica da consulta
3. Só depois de 2-3 buscas sem nada relevante, considere o assunto fora da base

QUANDO NÃO USAR:
- Conversa casual (saudações, agradecimentos) sem pergunta factual
- A informação já está no contexto desta conversa

Esgotadas as buscas sem resultado relevante: responda com base nas suas próprias instruções e no contexto da conversa; se a informação não estiver em lugar nenhum, diga ao cliente que não tem essa informação em vez de inventar.`,
		Parameters: map[string]tools.Parameter{
			"query": {
				Type:               "string",
				Description:        "A consulta de busca. Use os termos-chave do assunto em uma frase específica em linguagem natural (ex.: 'política de reembolso para planos anuais'), nunca palavras soltas e genéricas. Se uma busca anterior não encontrou nada, NÃO repita a mesma consulta: reformule com sinônimos ou termos mais amplos/específicos. Para fatos diferentes, chame a ferramenta várias vezes com uma consulta focada em cada fato.",
				DisplayName:        "Consulta",
				DisplayDescription: "O que buscar na base de conhecimento",
			},
		},
		Required:   []string{"query"},
		Visibility: []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:   tools.CategoryAgentUtility,
	}
}

func (t *searchKnowledgeBaseTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *searchKnowledgeBaseTool) ExecuteWithConfig(ctx context.Context, config, params map[string]interface{}) (tools.ExecutionResult, error) {
	// Identity comes from the seeded config, with the context agent as
	// fallback; the context agent also supplies the operator-tuned RAGConfig.
	agentID, _ := config["__agent_id"].(string)
	agentID = strings.TrimSpace(agentID)
	var ragCfg *agent.RAGConfig
	if ctxAgent, ok := agentctx.AgentFromContext(ctx); ok {
		if agentID == "" {
			agentID = ctxAgent.ID
		}
		ragCfg = ctxAgent.RAGConfig
	}
	if agentID == "" {
		return errResult("Não foi possível identificar o agente atual. Esta ferramenta só funciona durante um atendimento."), nil
	}

	query, _ := params["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return errResult("O parâmetro 'query' é obrigatório. Informe uma pergunta ou frase específica para buscar."), nil
	}

	cfg := ragCfg.WithDefaults()
	out, err := t.rag.QueryForAgent(ctx, rag.AgentQueryInput{
		AgentID:         agentID,
		Query:           query,
		TopK:            cfg.MaxChunks,
		MinScore:        float32(cfg.MinScore),
		IncludeMetadata: true,
		NumCandidates:   cfg.NumCandidates,
		ContextWindow:   cfg.ContextWindow,
		MaxChunksPerDoc: cfg.MaxChunksPerDoc,
		MaxCharacters:   cfg.MaxCharacters,
	})
	if err != nil {
		log.Printf("[SearchKnowledgeBase] agent=%s query=%q err=%v", agentID, query, err)
		return errResult("Erro ao consultar a base de conhecimento. Tente novamente."), nil
	}

	if len(out.Results) == 0 {
		return tools.ExecutionResult{
			Result: fmt.Sprintf("Nenhum resultado para %q. NÃO repita a mesma consulta: reformule com sinônimos ou termos-chave diferentes (mais amplos ou mais específicos, no vocabulário que os documentos da empresa usariam) e busque de novo. Só depois de 2-3 tentativas sem nada relevante, responda com base nas suas próprias instruções e no contexto da conversa — e não invente dados específicos (valores, preços, prazos) que não estejam disponíveis em nenhuma fonte.", query),
		}, nil
	}

	return tools.ExecutionResult{Result: formatSearchResults(query, out.Results)}, nil
}

// formatSearchResults renders ranked excerpts with numbered source markers so
// the model can ground and cite specific chunks in its reply.
func formatSearchResults(query string, results []rag.QueryResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Encontrados %d trechos para %q (ordenados por relevância):\n\n", len(results), query))
	for i, r := range results {
		if r.DocumentName != "" {
			sb.WriteString(fmt.Sprintf("[%d] Fonte: %s (relevância: %.2f)\n%s\n\n", i+1, r.DocumentName, r.Score, r.Content))
		} else {
			sb.WriteString(fmt.Sprintf("[%d] (relevância: %.2f)\n%s\n\n", i+1, r.Score, r.Content))
		}
	}
	sb.WriteString("Baseie sua resposta nesses trechos para dados específicos (valores, preços, datas, condições) e não invente o que não estiver neles.")
	return sb.String()
}

var _ tools.Handler = (*searchKnowledgeBaseTool)(nil)
