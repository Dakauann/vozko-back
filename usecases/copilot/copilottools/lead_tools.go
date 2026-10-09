package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/cache"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	lm "vozko/domain/lead_memory"
	"vozko/domain/leadarea"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

const leadMemoryLimit = 20

var errLeadToolUnavailable = errors.New("lead tool: a required port is not wired")

type LeadPages interface {
	List(ctx context.Context, a conversation.Viewer, in lead.ListLeadsInput) (*shared.PaginatedResult[*lead.LeadWithSummary], error)
}

type LeadSections interface {
	Summary(ctx context.Context, a conversation.Viewer, f crmfilter.Filter) (*lead.SummarySection, error)
	Places(ctx context.Context, a conversation.Viewer, f crmfilter.Filter) (*lead.PlacesSection, error)
}

type LeadAreas interface {
	List(ctx context.Context, a conversation.Viewer) ([]leadarea.Area, error)
}

type LeadDefinitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type LeadDeps struct {
	Leads       lead.Queries
	Memories    lm.ListUseCase
	Pages       LeadPages
	Sections    LeadSections
	Areas       LeadAreas
	Definitions LeadDefinitions
}

func leadReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceLeads, Action: workspace.ActionRead}
}

type searchLeadsArgs struct {
	leadFilterArgs
	Page int `json:"page" desc:"página, começa em 1"`
}

type searchLeadsTool struct{ deps LeadDeps }

func NewSearchLeadsTool(deps LeadDeps) copilot.Tool { return &searchLeadsTool{deps: deps} }

func (t *searchLeadsTool) Meta() copilot.Meta { return leadReadMeta() }

func (t *searchLeadsTool) Definition() tools.Definition {
	return definition("search_leads", fmt.Sprintf(
		"Busca contatos (clientes) do workspace, do mais recente para o mais antigo, %d por página: nome, número mascarado, "+
			"bairro, cidade, responsável, campanhas, memórias e última atividade. Filtra por texto, cidade, bairros, área "+
			"desenhada, responsável ou pelo filtro da tela de Leads. Use get_lead para os detalhes e as memórias de um contato.", searchPageSize),
		searchLeadsArgs{})
}

func (t *searchLeadsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a searchLeadsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if a.Page < 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "page começa em 1"}
	}
	if t.deps.Pages == nil {
		return leadFailure("search_leads", errLeadToolUnavailable)
	}
	page := max(a.Page, 1)
	filter, err := leadFilterOf(ctx, cc, t.deps, a.leadFilterArgs)
	if err != nil {
		return leadFailure("search_leads", err)
	}
	result, err := t.deps.Pages.List(ctx, viewerOf(cc), lead.ListLeadsInput{
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
			digests = append(digests, lead.DigestItem(item))
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
	if res, known := sharedLeadFailure(err); known {
		return res
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao consultar os contatos"}
}

func sharedLeadFailure(err error) (copilot.Result, bool) {
	failed := func(message string) (copilot.Result, bool) {
		return copilot.Result{Status: copilot.StatusError, Message: message}, true
	}
	denied := func(message string) (copilot.Result, bool) {
		return copilot.Result{Status: copilot.StatusDenied, Message: message}, true
	}
	switch {
	case errors.Is(err, errUnknownLead), errors.Is(err, errInvalidArgs):
		return failed(err.Error())
	case errors.Is(err, errLeadToolUnavailable):
		return failed("os leads não estão disponíveis neste servidor agora")
	case errors.Is(err, lead.ErrLeadForbidden):
		return denied("o usuário não tem permissão para ver leads")
	case errors.Is(err, lead.ErrLeadFilterAddressForbidden), errors.Is(err, leadarea.ErrAddressesRequired):
		return denied("isso exige permissão para ver endereços completos (filtros por CEP, área ou precisão do mapa, exportação com endereço)")
	case errors.Is(err, customfield.ErrFilterSensitive):
		return denied("o filtro usa um campo sensível; campos sensíveis nunca passam pela Elo")
	case errors.Is(err, leadarea.ErrNotFound):
		return failed("a área não existe mais ou não está visível para o usuário")
	case errors.Is(err, crmfilter.ErrConjunctionRequired):
		return failed("o filtro da tela tem um grupo de condições sem dizer se vale qualquer uma ou todas; peça ao usuário para escolher no filtro")
	case errors.Is(err, lead.ErrLeadFilterInvalid), errors.Is(err, crmfilter.ErrNotApplicable):
		return failed("o filtro não pode ser aplicado aos leads; confira os argumentos")
	case errors.Is(err, cache.ErrGateBusy):
		return failed("as consultas de leads estão ocupadas agora; tente de novo em alguns segundos")
	case errors.Is(err, context.DeadlineExceeded):
		return failed("a consulta demorou demais; use um filtro menor")
	}
	return copilot.Result{}, false
}
