package workflow_usecase

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/redact"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const (
	TriggerInputInvalidCode     = "trigger_input_invalid"
	TriggerNotFoundCode         = "trigger_not_found"
	TriggerInputUnavailableCode = "trigger_input_unavailable"
	NoRecentTriggerInputCode    = "no_recent_input"

	simLeadName = "Contato de teste"
)

type TriggerInputReaders struct {
	Forms  workflow.TriggerFormCatalog
	Stages workflow.TriggerStageCatalog
}

func (r TriggerInputReaders) Specs(workspaceID string, g *workflow.Graph) []workflow.TriggerInputSpec {
	needs := workflow.TriggerInputNeedsOf(g)
	sources := workflow.TriggerInputSources{Forms: map[string]workflow.TriggerForm{}, Unreadable: map[string]bool{}}
	unreadable := func(nodes []string, what string, err error) {
		for _, nodeID := range nodes {
			sources.Unreadable[nodeID] = true
			slog.Warn("workflow.trigger_input.source_unreadable",
				slog.String("workspace_id", workspaceID), slog.String("node_id", nodeID), slog.String("source", what), slog.String("error", err.Error()))
		}
	}
	if needs.AnyForm || len(needs.FormIDs) > 0 {
		r.readForms(workspaceID, needs, &sources, unreadable)
	}
	if needs.Stages {
		if r.Stages == nil {
			unreadable(needs.StageNodes, "stages", errTriggerSourceMissing)
		} else if stages, err := r.Stages.TriggerStages(workspaceID); err != nil {
			unreadable(needs.StageNodes, "stages", err)
		} else {
			sources.Stages = stages
		}
	}
	return workflow.TriggerInputSpecsOf(g, sources)
}

var errTriggerSourceMissing = errors.New("no reader for this trigger source is wired")

func (r TriggerInputReaders) readForms(workspaceID string, needs workflow.TriggerInputNeeds, sources *workflow.TriggerInputSources, unreadable func([]string, string, error)) {
	if r.Forms == nil {
		unreadable(needs.FormNodes, "forms", errTriggerSourceMissing)
		return
	}
	if needs.AnyForm {
		choices, err := r.Forms.TriggerFormChoices(workspaceID)
		if err != nil {
			unreadable(needs.FormNodes, "forms", err)
			return
		}
		sources.FormChoices = choices
	}
	for _, formID := range needs.FormIDs {
		form, err := r.Forms.TriggerForm(workspaceID, formID)
		if err != nil {
			unreadable(needs.FormNodes, "form "+formID, err)
			return
		}
		sources.Forms[formID] = form
	}
}

func startFromTrigger(specs []workflow.TriggerInputSpec, nodeID string, input map[string]interface{}, subject workflow.TriggerTestSubject) (workflow.TriggerInputSpec, map[string]interface{}, error) {
	spec, found := workflow.FindTriggerInputSpec(specs, strings.TrimSpace(nodeID))
	if !found {
		return workflow.TriggerInputSpec{}, nil, workflow.ErrTriggerInputNotFound
	}
	if !spec.Readable() {
		return workflow.TriggerInputSpec{}, nil, workflow.ErrTriggerInputUnavailable
	}
	data, issues := workflow.TriggerRunDataFor(spec, input, subject)
	if len(issues) > 0 {
		return workflow.TriggerInputSpec{}, nil, &workflow.TriggerInputInvalidError{Issues: issues}
	}
	return spec, data, nil
}

type triggerStartRefusal struct {
	Command string                       `json:"command"`
	Error   string                       `json:"error"`
	Code    string                       `json:"code,omitempty"`
	Issues  []workflow.TriggerInputIssue `json:"issues,omitempty"`
}

func refusalOf(command string, err error) triggerStartRefusal {
	if issues, invalid := workflow.TriggerInputIssuesOf(err); invalid {
		return triggerStartRefusal{Command: command, Code: TriggerInputInvalidCode, Issues: issues,
			Error: "Os dados de entrada não passam nas regras deste gatilho. Corrija os campos indicados."}
	}
	switch {
	case errors.Is(err, workflow.ErrTriggerInputNotFound):
		return triggerStartRefusal{Command: command, Code: TriggerNotFoundCode, Error: "Este gatilho não existe no rascunho."}
	case errors.Is(err, workflow.ErrTriggerInputUnavailable):
		return triggerStartRefusal{Command: command, Code: TriggerInputUnavailableCode,
			Error: "Não foi possível ler agora o que este gatilho recebe (formulários ou etapas). Tente de novo."}
	}
	return triggerStartRefusal{Command: command, Error: err.Error()}
}

type LastTriggerInput struct {
	Input      map[string]interface{} `json:"input"`
	MaskedKeys []string               `json:"maskedKeys"`
	RunID      string                 `json:"runId"`
	StartedAt  time.Time              `json:"startedAt"`
}

type LastTriggerInputUseCase struct {
	history    workflow.TriggerInputHistory
	visibility conversation.Visibility
	readers    TriggerInputReaders
}

func NewLastTriggerInputUseCase(history workflow.TriggerInputHistory, visibility conversation.Visibility, readers TriggerInputReaders) *LastTriggerInputUseCase {
	return &LastTriggerInputUseCase{history: history, visibility: visibility, readers: readers}
}

func (uc *LastTriggerInputUseCase) Execute(viewer shared.Person, wf *workflow.Workflow, nodeID string) (*LastTriggerInput, error) {
	node := wf.Graph.FindNode(strings.TrimSpace(nodeID))
	if node == nil || !node.Type.IsTrigger() {
		return nil, workflow.ErrTriggerInputNotFound
	}
	spec, found := workflow.FindTriggerInputSpec(uc.readers.Specs(wf.WorkspaceID, &wf.Graph), node.ID)
	if !found {
		return nil, workflow.ErrTriggerInputNotFound
	}
	if !spec.Readable() {
		return nil, workflow.ErrTriggerInputUnavailable
	}
	if uc.history == nil {
		return nil, workflow.ErrNoRecentTriggerInput
	}
	recorded, err := uc.history.LatestTriggerData(wf.WorkspaceID, wf.ID, workflow.TriggerType(node.Type), conversationScope(uc.visibility, viewer, wf.WorkspaceID))
	if err != nil {
		slog.Error("workflow.trigger_input.last_read_failed",
			slog.String("workspace_id", wf.WorkspaceID), slog.String("workflow_id", wf.ID), slog.String("node_id", node.ID), slog.String("error", err.Error()))
		return nil, err
	}
	if recorded == nil {
		return nil, workflow.ErrNoRecentTriggerInput
	}
	input := workflow.TriggerInputFromRunData(spec, redact.New().Map(recorded.Data))
	return &LastTriggerInput{
		Input:      input,
		MaskedKeys: workflow.MaskedTriggerInputKeys(spec, input),
		RunID:      recorded.RunID,
		StartedAt:  recorded.StartedAt,
	}, nil
}
