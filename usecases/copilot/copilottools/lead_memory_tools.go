package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/actor"
	"vozko/domain/copilot"
	"vozko/domain/lead"
	lm "vozko/domain/lead_memory"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type LeadMemoryDeps struct {
	Leads  lead.Queries
	Create lm.CreateUseCase
	Update lm.UpdateUseCase
}

type addLeadMemoryArgs struct {
	LeadID   string `json:"lead_id" req:"true" desc:"lead_id exato de search_leads" id:"true"`
	Content  string `json:"content" req:"true" desc:"o fato, curto e objetivo (ex.: prefere pagar no boleto)"`
	Category string `json:"category" req:"true" desc:"tipo do fato"`
}

type updateLeadMemoryArgs struct {
	LeadID   string `json:"lead_id" req:"true" desc:"lead_id exato de search_leads" id:"true"`
	MemoryID string `json:"memory_id" req:"true" desc:"memory_id exato de get_lead"`
	Content  string `json:"content" req:"true" desc:"o texto corrigido"`
	Category string `json:"category" req:"true" desc:"tipo do fato"`
}

type leadMemoryTool struct {
	deps   LeadMemoryDeps
	update bool
}

func NewAddLeadMemoryTool(deps LeadMemoryDeps) copilot.Tool { return &leadMemoryTool{deps: deps} }

func NewUpdateLeadMemoryTool(deps LeadMemoryDeps) copilot.Tool {
	return &leadMemoryTool{deps: deps, update: true}
}

func (t *leadMemoryTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceLeads, Action: workspace.ActionUpdate}
}

func (t *leadMemoryTool) Definition() tools.Definition {
	var def tools.Definition
	if t.update {
		def = definition("update_lead_memory", "Corrige uma memória existente de um contato. Só depois da aprovação do usuário.", updateLeadMemoryArgs{})
	} else {
		def = definition("add_lead_memory",
			"Registra um fato novo sobre um contato (preferência, objeção, combinado...), que a equipe e os agentes passam a ver. "+
				"Só depois da aprovação do usuário. Se já existir uma memória parecida, corrija-a com update_lead_memory.",
			addLeadMemoryArgs{})
	}
	param := def.Parameters["category"]
	for _, c := range lm.AllCategories() {
		param.Enum = append(param.Enum, string(c))
	}
	def.Parameters["category"] = param
	return def
}

func (t *leadMemoryTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updateLeadMemoryArgs
	bindArgs(args, &a)
	contact := "contato desconhecido"
	if l, err := resolveLead(t.deps.Leads, cc, a.LeadID); err == nil {
		contact = strings.TrimSpace(l.Name + " (" + shared.MaskContact(l.Number) + ")")
	}
	return []copilot.Field{
		{Key: "contact", Value: contact},
		{Key: "content", Value: strings.TrimSpace(a.Content)},
		{Key: "category", Value: a.Category},
	}
}

func (t *leadMemoryTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updateLeadMemoryArgs
	var err error
	if t.update {
		err = decodeArgs(args, &a)
	} else {
		var add addLeadMemoryArgs
		err = decodeArgs(args, &add)
		a = updateLeadMemoryArgs{LeadID: add.LeadID, Content: add.Content, Category: add.Category}
	}
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	category := lm.Category(a.Category)
	if !category.Valid() {
		return copilot.Result{Status: copilot.StatusError, Message: "category inválida"}
	}
	l, err := resolveLead(t.deps.Leads, cc, a.LeadID)
	if err != nil {
		return leadFailure(t.Definition().Name, err)
	}
	author := lm.WriteActor{Kind: actor.KindHuman, ID: cc.UserID}
	var memory *lm.LeadMemory
	if t.update {
		memory, err = t.deps.Update.Execute(ctx, lm.UpdateInput{
			WorkspaceID: cc.WorkspaceID, LeadID: l.ID, MemoryRef: strings.TrimSpace(a.MemoryID),
			Content: a.Content, Category: category, Actor: author,
		})
	} else {
		var created *lm.CreateResult
		created, err = t.deps.Create.Execute(ctx, lm.CreateInput{
			WorkspaceID: cc.WorkspaceID, LeadID: l.ID, Content: a.Content, Category: category, Actor: author,
		})
		if created != nil {
			memory = created.Memory
		}
	}
	if err != nil {
		return memoryFailure(t.Definition().Name, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"saved": true, "memory_id": memory.ID}}
}

func memoryFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, lm.ErrDuplicate):
		return copilot.Result{Status: copilot.StatusError, Message: "já existe uma memória equivalente; corrija a existente"}
	case errors.Is(err, lm.ErrLimitReached):
		return copilot.Result{Status: copilot.StatusError, Message: "este contato atingiu o limite de memórias; corrija ou remova uma"}
	case errors.Is(err, lm.ErrContentTooLong):
		return copilot.Result{Status: copilot.StatusError, Message: "texto longo demais; resuma o fato"}
	case errors.Is(err, lm.ErrContentRequired), errors.Is(err, lm.ErrInvalidCategory):
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("dados inválidos: %v", err)}
	case errors.Is(err, lm.ErrNotFound), errors.Is(err, lm.ErrAmbiguousID):
		return copilot.Result{Status: copilot.StatusError, Message: "memória desconhecida; use o memory_id de get_lead"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao salvar a memória"}
}

func (t *leadMemoryTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	var leadID, category string
	if t.update {
		a, err := validateArgs[updateLeadMemoryArgs](nil, cc, args)
		if err != nil {
			return err
		}
		leadID, category = a.LeadID, a.Category
	} else {
		a, err := validateArgs[addLeadMemoryArgs](nil, cc, args)
		if err != nil {
			return err
		}
		leadID, category = a.LeadID, a.Category
	}
	if !lm.Category(category).Valid() {
		return fmt.Errorf("%w: category inválida", errInvalidArgs)
	}
	if _, err := resolveLead(t.deps.Leads, cc, leadID); err != nil {
		return fmt.Errorf("%w: contato não encontrado; use o lead_id exato de search_leads", errInvalidArgs)
	}
	return nil
}
