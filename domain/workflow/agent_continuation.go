package workflow

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/redact"
)

const TopicAgentRunFinished = "workflow.run.agent_finished"

const AgentRunFinishedWorkers = 10

const maxFailureDetailRunes = 300

type ContinuationKind string

const (
	ContinuationNone    ContinuationKind = ""
	ContinuationResult  ContinuationKind = "result"
	ContinuationFailure ContinuationKind = "failure"
)

type AgentRunFinished struct {
	RunID          string    `json:"runId"`
	WorkflowID     string    `json:"workflowId"`
	WorkspaceID    string    `json:"workspaceId"`
	EntryID        string    `json:"entryId"`
	EntryType      string    `json:"entryType"`
	AgentID        string    `json:"agentId"`
	Status         RunStatus `json:"status"`
	HasAgentResult bool      `json:"hasAgentResult"`
	FinishedAt     time.Time `json:"finishedAt"`
}

type AgentRunFinishedPublisher interface {
	PublishAgentRunFinished(event AgentRunFinished) error
}

const MaxAgentContinuationAttempts = 3

type AgentContinuationClaims interface {
	ClaimAgentContinuation(runID string) (bool, error)
	ReleaseAgentContinuation(runID string) (bool, error)
}

type ReplyRouting struct {
	RunID      string
	WorkflowID string
}

func AgentRunFinishedOf(run *WorkflowRun) (AgentRunFinished, bool) {
	if run == nil || !run.Status.IsTerminal() || !StartedByAnAgent(run.StartedBy) {
		return AgentRunFinished{}, false
	}
	agentID := strings.TrimSpace(strings.TrimPrefix(run.StartedBy, startedByAgent))
	if agentID == "" {
		return AgentRunFinished{}, false
	}
	return AgentRunFinished{
		RunID:          run.ID,
		WorkflowID:     run.WorkflowID,
		WorkspaceID:    run.WorkspaceID,
		EntryID:        run.EntryID,
		EntryType:      run.EntryType,
		AgentID:        agentID,
		Status:         run.Status,
		HasAgentResult: len(run.AgentResult) > 0,
		FinishedAt:     run.UpdatedAt,
	}, true
}

func (e AgentRunFinished) Continuation() ContinuationKind {
	switch {
	case e.Status == RunStatusError:
		return ContinuationFailure
	case e.Status == RunStatusCompleted && e.HasAgentResult:
		return ContinuationResult
	}
	return ContinuationNone
}

func ContinuationNote(kind ContinuationKind, workflowName, failure string) string {
	name := fmt.Sprintf("%q", strings.TrimSpace(workflowName))
	switch kind {
	case ContinuationResult:
		return fmt.Sprintf("O fluxo %s que você executou retornou agora, com o resultado acima. Continue a conversa com o cliente a partir desse resultado, sem repetir o que já foi dito.", name)
	case ContinuationFailure:
		return fmt.Sprintf("O fluxo %s que você executou falhou: %s. Continue a conversa com o cliente com naturalidade; não invente um status nem um resultado e diga que vai verificar.", name, strings.TrimSpace(failure))
	}
	return ""
}

var engineFailures = []struct {
	match []string
	text  string
}{
	{[]string{"run stuck"}, "o fluxo ficou parado sem avançar por tempo demais"},
	{[]string{ErrMaxExecutionsReached.Error(), ErrDurableExecutionsReached.Error(), "loop detected"}, "o fluxo foi interrompido por repetir etapas demais"},
	{[]string{ErrAutomationStateUnreadable.Error()}, "não foi possível confirmar se a automação da conversa está ligada"},
	{[]string{"node not found", "workflow not found"}, "o fluxo foi alterado ou removido durante a execução"},
}

func HumanRunFailure(errText string) string {
	errText = strings.TrimSpace(errText)
	if errText == "" {
		return "o fluxo parou com um erro"
	}
	for _, known := range engineFailures {
		for _, m := range known.match {
			if strings.Contains(errText, m) {
				return known.text
			}
		}
	}
	return "o fluxo parou com um erro: " + cut(redact.New().String(errText), maxFailureDetailRunes)
}

const (
	ContinuationSweepPeriod = 5 * time.Minute
	ContinuationSweepWindow = 15 * time.Minute
	ContinuationSweepGrace  = time.Minute
	ContinuationSweepLimit  = 500
)

type AgentContinuationBacklog interface {
	FinishedWithoutContinuation(finishedFrom, finishedBefore time.Time, limit int) ([]*WorkflowRun, error)
}
