package copilottools

import (
	"context"
	"errors"
	"log"
	"math"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/rag"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const (
	knowledgeResults      = 5
	knowledgeExcerptRunes = 600
	knowledgeListPageSize = 20
)

type KnowledgeDeps struct {
	List  rag.ListKnowledgeBasesUseCase
	Query rag.ScopedQueryUseCase
}

func knowledgeReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceKnowledgeBases, Action: workspace.ActionRead}
}

type listKnowledgeBasesArgs struct {
	DepartmentID string `json:"department_id" desc:"departamento (list_departments); omita para o do usuário"`
	Page         int    `json:"page" desc:"página, começa em 1"`
}

type listKnowledgeBasesTool struct{ deps KnowledgeDeps }

func NewListKnowledgeBasesTool(deps KnowledgeDeps) copilot.Tool {
	return &listKnowledgeBasesTool{deps: deps}
}

func (t *listKnowledgeBasesTool) Meta() copilot.Meta { return knowledgeReadMeta() }

func (t *listKnowledgeBasesTool) Definition() tools.Definition {
	return definition("list_knowledge_bases",
		"Lista as bases de conhecimento que o usuário pode ver (nome, descrição, documentos). Use antes de search_knowledge.",
		listKnowledgeBasesArgs{})
}

func (t *listKnowledgeBasesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listKnowledgeBasesArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	department, err := cc.Departments.ReadScope(strings.TrimSpace(a.DepartmentID))
	if err != nil {
		return knowledgeFailure("list_knowledge_bases", err)
	}
	var scope *string
	if department != "" {
		scope = &department
	}
	page := max(a.Page, 1)
	out, err := t.deps.List.Execute(ctx, cc.WorkspaceID, scope, page, knowledgeListPageSize)
	if err != nil {
		return knowledgeFailure("list_knowledge_bases", err)
	}
	bases := make([]map[string]interface{}, 0, len(out.Items))
	for _, kb := range out.Items {
		if kb == nil {
			continue
		}
		bases = append(bases, map[string]interface{}{
			"knowledge_base_id": kb.ID,
			"name":              kb.Name,
			"description":       clipText(kb.Description, 200),
			"documents":         kb.DocumentCount,
			"status":            kb.Status,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"page":            page,
		"total":           out.Total,
		"has_more":        page < out.TotalPages,
		"knowledge_bases": bases,
	}}
}

type searchKnowledgeArgs struct {
	Query            string   `json:"query" req:"true" desc:"o que procurar, em linguagem natural"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids" req:"true" desc:"ids exatos de list_knowledge_bases"`
}

type searchKnowledgeTool struct{ deps KnowledgeDeps }

func NewSearchKnowledgeTool(deps KnowledgeDeps) copilot.Tool {
	return &searchKnowledgeTool{deps: deps}
}

func (t *searchKnowledgeTool) Meta() copilot.Meta { return knowledgeReadMeta() }

func (t *searchKnowledgeTool) Definition() tools.Definition {
	return definition("search_knowledge",
		"Procura nas bases de conhecimento os trechos mais relevantes para uma pergunta e devolve os trechos com o documento de origem. "+
			"Responda com base nos trechos e cite o documento; se nada relevante vier, diga que a base não cobre o assunto.",
		searchKnowledgeArgs{})
}

func (t *searchKnowledgeTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a searchKnowledgeArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	out, err := t.deps.Query.Execute(ctx, rag.Viewer{WorkspaceID: cc.WorkspaceID, Departments: cc.Departments}, rag.QueryInput{
		KnowledgeBaseIDs: a.KnowledgeBaseIDs,
		Query:            a.Query,
		TopK:             knowledgeResults,
	})
	if err != nil {
		return knowledgeFailure("search_knowledge", err)
	}
	excerpts := make([]map[string]interface{}, 0, len(out.Results))
	for _, r := range out.Results {
		excerpts = append(excerpts, map[string]interface{}{
			"document":          r.DocumentName,
			"knowledge_base_id": r.KnowledgeBaseID,
			"relevance":         math.Round(float64(r.Score)*100) / 100,
			"text":              clipText(r.Content, knowledgeExcerptRunes),
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"found": out.TotalFound, "excerpts": excerpts}}
}

func clipText(s string, max int) string {
	text, cut := shared.TruncateRunes(strings.TrimSpace(s), max)
	if cut {
		return text + "…"
	}
	return text
}

func knowledgeFailure(tool string, err error) copilot.Result {
	if res, ok := departmentFailure(err); ok {
		return res
	}
	switch {
	case errors.Is(err, rag.ErrKnowledgeBaseAccessDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a essa base de conhecimento"}
	case errors.Is(err, rag.ErrKnowledgeBaseNotFound), errors.Is(err, rag.ErrKnowledgeBaseIDRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "base de conhecimento desconhecida; use os ids de list_knowledge_bases"}
	case errors.Is(err, rag.ErrQueryTooManyBases):
		return copilot.Result{Status: copilot.StatusError, Message: "bases demais numa busca; escolha as mais relevantes"}
	case errors.Is(err, rag.ErrQueryRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "query é obrigatório"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar as bases de conhecimento"}
}
