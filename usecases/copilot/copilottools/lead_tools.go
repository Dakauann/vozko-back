package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	lm "vozko/domain/lead_memory"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const leadMemoryLimit = 20

type LeadDeps struct {
	Leads    lead.Queries
	Memories lm.ListUseCase
}

func leadReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
}

type searchLeadsArgs struct {
	Query     string `json:"query" desc:"nome, número ou trecho das memórias do contato"`
	HasMemory bool   `json:"has_memory" desc:"só contatos com memórias registradas"`
	Page      int    `json:"page" desc:"página, começa em 1"`
}

type searchLeadsTool struct{ deps LeadDeps }

func NewSearchLeadsTool(deps LeadDeps) copilot.Tool { return &searchLeadsTool{deps: deps} }

func (t *searchLeadsTool) Meta() copilot.Meta { return leadReadMeta() }

func (t *searchLeadsTool) Definition() tools.Definition {
	return definition("search_leads", fmt.Sprintf(
		"Busca contatos (clientes) do workspace, do mais recente para o mais antigo, %d por página: nome, número mascarado, "+
			"campanhas, memórias e última atividade. Use get_lead para os detalhes e as memórias de um contato.", searchPageSize),
		searchLeadsArgs{})
}

func (t *searchLeadsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a searchLeadsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if a.Page < 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "page começa em 1"}
	}
	page := max(a.Page, 1)
	var filter crmfilter.Filter
	if q := strings.TrimSpace(a.Query); q != "" {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldQuery, crmfilter.OpContains, q))
	}
	if a.HasMemory {
		filter.Groups = append(filter.Groups, predicate(crmfilter.FieldMemoryCategory, crmfilter.OpIsSet))
	}
	result, err := t.deps.Leads.List(lead.ListLeadsInput{
		WorkspaceID: cc.WorkspaceID,
		Filter:      filter,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: page, PageSize: searchPageSize},
			Sorts:      []shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}},
		},
	})
	if err != nil {
		return leadFailure("search_leads", err)
	}
	digests := make([]lead.LeadDigest, 0, len(result.Items))
	for _, item := range result.Items {
		if item != nil && item.Lead != nil {
			digests = append(digests, lead.Digest(item.Lead, item.Summary))
		}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"page":     page,
		"total":    result.TotalItems,
		"has_more": page < result.TotalPages,
		"leads":    digests,
	}}
}

func predicate(field crmfilter.Field, op crmfilter.Operator, values ...string) crmfilter.Group {
	return crmfilter.Group{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: field, Operator: op, Values: values}}}
}

type getLeadArgs struct {
	LeadID string `json:"lead_id" req:"true" desc:"lead_id exato devolvido por search_leads"`
}

type getLeadTool struct{ deps LeadDeps }

func NewGetLeadTool(deps LeadDeps) copilot.Tool { return &getLeadTool{deps: deps} }

func (t *getLeadTool) Meta() copilot.Meta { return leadReadMeta() }

func (t *getLeadTool) Definition() tools.Definition {
	return definition("get_lead", fmt.Sprintf(
		"Detalhes de um contato e as %d memórias mais recentes sobre ele (preferências, objeções, combinados). "+
			"Para as conversas do contato use search_conversations com lead_id.", leadMemoryLimit),
		getLeadArgs{})
}

func (t *getLeadTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a getLeadArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	l, err := resolveLead(t.deps.Leads, cc, a.LeadID)
	if err != nil {
		return leadFailure("get_lead", err)
	}
	memories, err := t.deps.Memories.Execute(ctx, lm.ListInput{WorkspaceID: cc.WorkspaceID, LeadID: l.ID, Query: lm.ListQuery{Limit: leadMemoryLimit}})
	if err != nil {
		return leadFailure("get_lead", err)
	}
	digests := make([]lm.MemoryDigest, 0, len(memories.Items))
	for _, m := range memories.Items {
		digests = append(digests, lm.DigestMemory(m))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"lead":           lead.Digest(l, nil),
		"memories":       digests,
		"memories_total": memories.Total,
	}}
}

var errUnknownLead = errors.New("contato não encontrado; use o lead_id exato de search_leads")

func resolveLead(leads lead.Queries, cc copilot.Context, raw string) (*lead.Lead, error) {
	id := strings.TrimSpace(raw)
	if _, err := uuid.Parse(id); err != nil {
		return nil, errUnknownLead
	}
	l, err := leads.Get(cc.WorkspaceID, id)
	if errors.Is(err, lead.ErrLeadNotFound) {
		return nil, errUnknownLead
	}
	return l, err
}

func leadFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, errUnknownLead), errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	case errors.Is(err, lead.ErrLeadFilterInvalid):
		return copilot.Result{Status: copilot.StatusError, Message: "filtro inválido"}
	case errors.Is(err, context.DeadlineExceeded):
		return copilot.Result{Status: copilot.StatusError, Message: "a busca demorou demais; use filtros mais específicos"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar os contatos"}
}
