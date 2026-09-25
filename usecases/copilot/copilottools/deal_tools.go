package copilottools

import (
	"context"
	"errors"
	"log"
	"math"
	"strconv"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/copilot"
	"vozko/domain/opportunity"
	"vozko/domain/pipeline"
	"vozko/domain/stage"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	opportunity_usecase "vozko/usecases/opportunity"
)

type DealDeps struct {
	Deals     opportunity.PersonDealsUseCase
	Pipelines pipeline.ListPipelinesUseCase
	Stages    stage.ListStagesUseCase
	Entries   conversation.EntryLookup
}

func dealMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceConversations, Action: action}
}

func (d DealDeps) stageNames(cc copilot.Context, pipelineID string) map[string]string {
	names := map[string]string{}
	stages, err := d.Stages.Execute(cc.WorkspaceID, "", "", pipelineID)
	if err != nil {
		return names
	}
	for _, s := range stages {
		if s != nil {
			names[s.ID] = s.Name
		}
	}
	return names
}

const dealListLimit = 50

func money(cents int64) float64 { return float64(cents) / 100 }

type listDealPipelinesTool struct{ deps DealDeps }

func NewListDealPipelinesTool(deps DealDeps) copilot.Tool { return &listDealPipelinesTool{deps: deps} }

func (t *listDealPipelinesTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceStages, Action: workspace.ActionRead}
}

func (t *listDealPipelinesTool) Definition() tools.Definition {
	return definition("list_deal_pipelines", "Lista os funis de vendas (negócios) com as etapas de cada um.", struct{}{})
}

func (t *listDealPipelinesTool) Execute(_ context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	pipelines, err := t.deps.Pipelines.Execute(cc.WorkspaceID, string(pipeline.ObjectOpportunity))
	if err != nil {
		return dealFailure("list_deal_pipelines", err)
	}
	out := make([]map[string]interface{}, 0, len(pipelines))
	for _, p := range pipelines {
		if p == nil {
			continue
		}
		stages := []map[string]interface{}{}
		for id, name := range t.deps.stageNames(cc, p.ID) {
			stages = append(stages, map[string]interface{}{"stage_id": id, "name": name})
		}
		out = append(out, map[string]interface{}{"pipeline_id": p.ID, "name": p.Name, "default": p.IsDefault, "stages": stages})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"pipelines": out}}
}

type listDealsArgs struct {
	PipelineID string `json:"pipeline_id" req:"true" desc:"pipeline_id exato de list_deal_pipelines" id:"true"`
}

type listDealsTool struct{ deps DealDeps }

func NewListDealsTool(deps DealDeps) copilot.Tool { return &listDealsTool{deps: deps} }

func (t *listDealsTool) Meta() copilot.Meta { return dealMeta(workspace.ActionRead, false) }

func (t *listDealsTool) Definition() tools.Definition {
	return definition("list_deals", "Lista os negócios de um funil de vendas que o usuário pode ver: título, etapa, valor e status.", listDealsArgs{})
}

func (t *listDealsTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a listDealsArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	pipelineID, err := knownID(a.PipelineID, "pipeline_id", "list_deal_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	deals, err := t.deps.Deals.ListByPipeline(personOf(cc), cc.WorkspaceID, pipelineID)
	if err != nil {
		return dealFailure("list_deals", err)
	}
	names := t.deps.stageNames(cc, pipelineID)
	rows := make([]map[string]interface{}, 0, len(deals))
	for _, d := range deals {
		if d == nil {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"deal_id": d.ID, "title": d.Title, "stage": orUnknown(names[d.StageID], "etapa desconhecida"),
			"value": money(d.ValueCents), "currency": d.Currency, "status": d.Status,
		})
	}
	total := len(rows)
	if total > dealListLimit {
		rows = rows[:dealListLimit]
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"total": total, "has_more": total > dealListLimit, "deals": rows}}
}

type createDealArgs struct {
	PipelineID string  `json:"pipeline_id" req:"true" desc:"pipeline_id exato de list_deal_pipelines" id:"true"`
	StageID    string  `json:"stage_id" req:"true" desc:"stage_id exato de list_deal_pipelines" id:"true"`
	Title      string  `json:"title" req:"true" desc:"nome do negócio"`
	Value      float64 `json:"value" desc:"valor na moeda do workspace (ex.: 1200.50)"`
	LeadID     string  `json:"lead_id" desc:"lead_id de search_leads, quando o negócio é de um contato" id:"true"`
	EntryID    string  `json:"entry_id" desc:"entry_id de search_conversations, para ligar o negócio à conversa"`
	EntryType  string  `json:"entry_type" desc:"entry_type da conversa, junto com entry_id"`
}

type createDealTool struct{ deps DealDeps }

func NewCreateDealTool(deps DealDeps) copilot.Tool { return &createDealTool{deps: deps} }

func (t *createDealTool) Meta() copilot.Meta { return dealMeta(workspace.ActionCreate, true) }

func (t *createDealTool) Definition() tools.Definition {
	return definition("create_deal", "Cria um negócio num funil de vendas, opcionalmente ligado a uma conversa. Só depois da aprovação do usuário.", createDealArgs{})
}

func (t *createDealTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createDealArgs
	bindArgs(args, &a)
	fields := []copilot.Field{
		{Key: "title", Value: strings.TrimSpace(a.Title)},
		{Key: "stage", Value: orUnknown(t.deps.stageNames(cc, strings.TrimSpace(a.PipelineID))[strings.TrimSpace(a.StageID)], "etapa desconhecida")},
	}
	if a.Value > 0 {
		fields = append(fields, copilot.Field{Key: "value", Value: formatAmount(a.Value)})
	}
	if strings.TrimSpace(a.EntryID) != "" {
		fields = append(fields, copilot.Field{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)})
	}
	return fields
}

func (t *createDealTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createDealArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	pipelineID, err := knownID(a.PipelineID, "pipeline_id", "list_deal_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stageID, err := knownID(a.StageID, "stage_id", "list_deal_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if a.Value < 0 {
		return copilot.Result{Status: copilot.StatusError, Message: "o valor não pode ser negativo"}
	}
	draft := opportunity.DealDraft{PipelineID: pipelineID, StageID: stageID, Title: strings.TrimSpace(a.Title), ValueCents: int64(math.Round(a.Value * 100))}
	if strings.TrimSpace(a.LeadID) != "" {
		if draft.LeadID, err = knownID(a.LeadID, "lead_id", "search_leads"); err != nil {
			return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
		}
	}
	if strings.TrimSpace(a.EntryID) != "" {
		target, err := targetOf(a.EntryID, a.EntryType)
		if err != nil {
			return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
		}
		draft.EntryID, draft.EntryType = target.EntryID, string(target.EntryType)
	}
	created, err := t.deps.Deals.Create(personOf(cc), cc.WorkspaceID, draft)
	if err != nil {
		return dealFailure("create_deal", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deal_id": created.ID, "title": created.Title}}
}

type moveDealArgs struct {
	DealID     string `json:"deal_id" req:"true" desc:"deal_id exato de list_deals" id:"true"`
	PipelineID string `json:"pipeline_id" req:"true" desc:"pipeline_id do negócio" id:"true"`
	StageID    string `json:"stage_id" req:"true" desc:"stage_id de destino, do mesmo funil" id:"true"`
}

type moveDealTool struct{ deps DealDeps }

func NewMoveDealTool(deps DealDeps) copilot.Tool { return &moveDealTool{deps: deps} }

func (t *moveDealTool) Meta() copilot.Meta { return dealMeta(workspace.ActionUpdate, true) }

func (t *moveDealTool) Definition() tools.Definition {
	return definition("move_deal", "Move um negócio para outra etapa do funil de vendas. Só depois da aprovação do usuário.", moveDealArgs{})
}

func (t *moveDealTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a moveDealArgs
	bindArgs(args, &a)
	title := "negócio desconhecido"
	if deal, err := t.deps.Deals.Get(personOf(cc), cc.WorkspaceID, strings.TrimSpace(a.DealID)); err == nil && deal != nil {
		title = deal.Title
	}
	return []copilot.Field{
		{Key: "deal", Value: title},
		{Key: "stage", Value: orUnknown(t.deps.stageNames(cc, strings.TrimSpace(a.PipelineID))[strings.TrimSpace(a.StageID)], "etapa desconhecida")},
	}
}

func (t *moveDealTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a moveDealArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	dealID, err := knownID(a.DealID, "deal_id", "list_deals")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	stageID, err := knownID(a.StageID, "stage_id", "list_deal_pipelines")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if _, err := t.deps.Deals.Move(personOf(cc), cc.WorkspaceID, dealID, stageID); err != nil {
		return dealFailure("move_deal", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"moved": true}}
}

type linkDealArgs struct {
	DealID    string `json:"deal_id" req:"true" desc:"deal_id exato de list_deals" id:"true"`
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
}

type linkDealTool struct{ deps DealDeps }

func NewLinkDealTool(deps DealDeps) copilot.Tool { return &linkDealTool{deps: deps} }

func (t *linkDealTool) Meta() copilot.Meta { return dealMeta(workspace.ActionUpdate, true) }

func (t *linkDealTool) Definition() tools.Definition {
	return definition("link_deal", "Liga uma conversa a um negócio existente. Só depois da aprovação do usuário.", linkDealArgs{})
}

func (t *linkDealTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a linkDealArgs
	bindArgs(args, &a)
	title := "negócio desconhecido"
	if deal, err := t.deps.Deals.Get(personOf(cc), cc.WorkspaceID, strings.TrimSpace(a.DealID)); err == nil && deal != nil {
		title = deal.Title
	}
	return []copilot.Field{
		{Key: "deal", Value: title},
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
	}
}

func (t *linkDealTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a linkDealArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	dealID, err := knownID(a.DealID, "deal_id", "list_deals")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.Deals.Link(personOf(cc), cc.WorkspaceID, dealID, target.EntryID, string(target.EntryType)); err != nil {
		return dealFailure("link_deal", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"linked": true}}
}

func formatAmount(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1)
}

func dealFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, opportunity.ErrEntryAccess):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa"}
	case errors.Is(err, opportunity.ErrScopeDenied):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso aos negócios deste workspace"}
	case errors.Is(err, opportunity.ErrNotFound), errors.Is(err, opportunity_usecase.ErrPipelineNotFound),
		errors.Is(err, opportunity_usecase.ErrStageNotFound), errors.Is(err, opportunity.ErrStageOutsidePipeline):
		return copilot.Result{Status: copilot.StatusError, Message: "negócio, funil ou etapa desconhecido; use os ids de list_deal_pipelines e list_deals"}
	case errors.Is(err, opportunity_usecase.ErrNotOpportunityPipeline):
		return copilot.Result{Status: copilot.StatusError, Message: "esse funil não é de vendas; use list_deal_pipelines"}
	case errors.Is(err, opportunity_usecase.ErrLeadOutsideWorkspace), errors.Is(err, opportunity_usecase.ErrEntryOutsideWorkspace):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o contato ou a conversa não é deste workspace"}
	case errors.Is(err, opportunity.ErrLostReasonMissing):
		return copilot.Result{Status: copilot.StatusError, Message: "para marcar como perdido é preciso um motivo de perda; faça pela tela de vendas"}
	case errors.Is(err, opportunity.ErrWonWithoutValue):
		return copilot.Result{Status: copilot.StatusError, Message: "para marcar como ganho o negócio precisa de um valor"}
	case errors.Is(err, opportunity.ErrTitleOrLead):
		return copilot.Result{Status: copilot.StatusError, Message: "informe um título ou um contato"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao mudar os negócios"}
}

func (t *createDealTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[createDealArgs](t.deps.Entries, cc, args)
	return err
}

func (t *moveDealTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[moveDealArgs](nil, cc, args)
	return err
}

func (t *linkDealTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[linkDealArgs](t.deps.Entries, cc, args)
	return err
}
