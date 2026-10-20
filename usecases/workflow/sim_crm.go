package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	ia "vozko/domain/inbox_assignment"
	label_domain "vozko/domain/label"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	stage_domain "vozko/domain/stage"
	"vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
	"vozko/usecases/workflow/node_executors"
)

var errSandboxCannotResolve = errors.New("o teste não consegue ler o cadastro deste workspace para conferir o alvo; nada foi alterado")

type DealPreviewer interface {
	PreviewForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryPreview, error)
}

type LeadProfilePreviewer interface {
	Preview(in lead_usecase.ProfileUpdate) (lead_usecase.ProfilePreview, error)
}

var (
	_ label_domain.AutomationLabeler       = simLabeler{}
	_ stage_domain.AssignEntryStageUseCase = simStageMover{}
	_ node_executors.ConversationHandOff   = simHandOff{}
	_ node_executors.DealDesk              = simDeals{}
	_ node_executors.LeadProfileWriter     = simLeadProfiles{}
)

type simLabeler struct {
	labels  label_domain.Repository
	effects *simEffects
}

func (l simLabeler) Change(workspaceID string, req label_domain.LabelChangeRequest) (label_domain.LabelChange, error) {
	if l.labels == nil {
		return label_domain.LabelChange{}, errSandboxCannotResolve
	}
	found, err := l.labels.FindByID(req.LabelID)
	if err != nil || found == nil || found.WorkspaceID != workspaceID {
		return label_domain.LabelChange{}, fmt.Errorf("%w: %s", label_domain.ErrLabelNotFound, req.LabelID)
	}
	verb := "Adicionaria"
	if req.Action == label_domain.LabelActionRemove {
		verb = "Removeria"
	}
	l.effects.change(workflow.SimulatedChange{
		Kind: workflow.SimulatedLabel, Action: string(req.Action), TargetID: found.ID, TargetName: found.Name,
		Description: fmt.Sprintf("%s a etiqueta %s", verb, found.Name),
	})
	return label_domain.LabelChange{LabelID: found.ID, LabelName: found.Name}, nil
}

type simStageMover struct {
	stages  node_executors.StageReader
	effects *simEffects
}

func (m simStageMover) Execute(workspaceID string, input stage_domain.AssignEntryStageInput) (*stage_domain.EntryStage, error) {
	if m.stages == nil {
		return nil, errSandboxCannotResolve
	}
	target, err := m.stages.FindByID(input.StageID)
	if err != nil || target == nil || target.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("%w: %s", stage_domain.ErrTagNotFound, input.StageID)
	}
	m.effects.change(workflow.SimulatedChange{
		Kind: workflow.SimulatedStage, Action: "move", TargetID: target.ID, TargetName: target.Name,
		Description: fmt.Sprintf("Moveria a conversa para a etapa %s", target.Name),
	})
	return &stage_domain.EntryStage{
		StageID: target.ID, StageName: target.Name, EntryID: input.EntryID, EntryType: input.EntryType,
		WorkspaceID: workspaceID, CreatedAt: time.Now().UTC(),
	}, nil
}

type simHandOff struct {
	members     workspace_domain.Repository
	departments dept_domain.Repository
	effects     *simEffects
}

func (h simHandOff) HandOffToHuman(workspaceID, _, _, toUserID string) error {
	if h.members == nil {
		return errSandboxCannotResolve
	}
	member, err := h.members.GetMember(workspaceID, toUserID)
	if err != nil || member == nil {
		return fmt.Errorf("membro %q não encontrado no workspace", toUserID)
	}
	name := member.Email
	if name == "" {
		name = member.Username
	}
	h.effects.change(workflow.SimulatedChange{
		Kind: workflow.SimulatedMember, Action: "assign", TargetID: member.UserID, TargetName: name,
		Description: fmt.Sprintf("Atribuiria a conversa a %s", name),
	})
	return nil
}

func (h simHandOff) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	departmentID := strings.TrimSpace(in.DepartmentID)
	if departmentID == "" {
		h.effects.change(workflow.SimulatedChange{
			Kind: workflow.SimulatedDepartment, Action: "transfer",
			Description: "Devolveria a conversa para a fila do departamento atual dela",
		})
		return "", nil
	}
	if h.departments == nil {
		return "", errSandboxCannotResolve
	}
	found, err := h.departments.GetDepartmentByID(departmentID)
	if err != nil || found == nil || found.WorkspaceID != in.WorkspaceID {
		return "", fmt.Errorf("department %s: %w", departmentID, ia.ErrDepartmentOutOfScope)
	}
	h.effects.change(workflow.SimulatedChange{
		Kind: workflow.SimulatedDepartment, Action: "transfer", TargetID: found.ID, TargetName: found.Name,
		Description: fmt.Sprintf("Transferiria a conversa para o departamento %s", found.Name),
	})
	return "", nil
}

type simDeals struct {
	previews DealPreviewer
	effects  *simEffects
}

func (d simDeals) DealsForEntry(_, _, _, _ string) (opportunity.EntryDeals, error) {
	return opportunity.EntryDeals{}, nil
}

var simDealVerbs = map[opportunity_usecase.EntryAction]string{
	opportunity_usecase.EntryCreate:      "Criaria ou atualizaria a oportunidade desta conversa",
	opportunity_usecase.EntryCreateNew:   "Criaria uma nova oportunidade",
	opportunity_usecase.EntryUpdateValue: "Atualizaria a oportunidade aberta",
	opportunity_usecase.EntryMove:        "Moveria a oportunidade aberta",
	opportunity_usecase.EntryWin:         "Marcaria a oportunidade como ganha",
	opportunity_usecase.EntryLose:        "Marcaria a oportunidade como perdida",
}

func (d simDeals) ManageForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
	if d.previews == nil {
		return nil, errSandboxCannotResolve
	}
	preview, err := d.previews.PreviewForEntry(workspaceID, cmd)
	if err != nil {
		return nil, err
	}
	deal := &opportunity.Opportunity{PipelineID: preview.PipelineID, Title: preview.Title, Status: opportunity.StatusOpen, LostReasonID: preview.LostReasonID}
	if preview.ValueCents != nil {
		deal.ValueCents = *preview.ValueCents
	}
	change := workflow.SimulatedChange{Kind: workflow.SimulatedDeal, Action: string(cmd.Action), Description: simDealVerbs[cmd.Action]}
	if preview.Stage != nil {
		deal.StageID = preview.Stage.ID
		switch {
		case preview.Stage.IsWon:
			deal.Status = opportunity.StatusWon
		case preview.Stage.IsLost:
			deal.Status = opportunity.StatusLost
		}
		change.TargetID, change.TargetName = preview.Stage.ID, preview.Stage.Name
		change.Description += fmt.Sprintf(" na etapa %s", preview.Stage.Name)
	}
	if preview.ValueCents != nil {
		change.Field, change.Value = "value_cents", *preview.ValueCents
	}
	d.effects.change(change)
	created := cmd.Action == opportunity_usecase.EntryCreate || cmd.Action == opportunity_usecase.EntryCreateNew || cmd.Action == opportunity_usecase.EntryWin
	return &opportunity_usecase.EntryResult{Opportunity: deal, Created: created}, nil
}

type simLeadProfiles struct {
	previews LeadProfilePreviewer
	effects  *simEffects
}

func (p simLeadProfiles) UpdateOfEntry(_ context.Context, _ shared.EntryRef, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	if p.previews == nil {
		return lead_usecase.ProfileResult{}, errSandboxCannotResolve
	}
	preview, err := p.previews.Preview(in)
	if err != nil {
		return lead_usecase.ProfileResult{}, err
	}
	for _, field := range preview.Changed {
		p.effects.change(workflow.SimulatedChange{
			Kind: workflow.SimulatedLeadField, Action: "update", Field: field, Value: preview.Values[field],
			Description: fmt.Sprintf("Gravaria o campo %s do lead", field),
		})
	}
	return lead_usecase.ProfileResult{Changed: preview.Changed, Conflicts: preview.Conflicts}, nil
}
