package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const CancelReasonConversationFinished = "conversation_finished"

const (
	maxAgentResultRunes = 2000
	maxAgentErrorRunes  = 500
	maxQuestionRunes    = 500
)

type AgentCallRun struct {
	RunID           string
	WorkflowID      string
	WorkflowName    string
	Status          RunStatus
	CurrentNodeID   string
	CurrentNodeType NodeType
	CurrentStepName string
	WaitReason      WaitReason
	WakeAt          *int64
	LastQuestion    string
	AgentResult     map[string]interface{}
	Error           string
	StartedAt       time.Time
	FinishedAt      *time.Time
}

type AgentCallRuns interface {
	LatestForEntry(workspaceID, entryID, entryType string) (*AgentCallRun, error)
}

type CancelledAgentRun struct {
	RunID         string
	WorkspaceID   string
	WorkflowID    string
	CurrentNodeID string
}

type AgentRunCanceller interface {
	CancelActiveAgentRuns(entryID, entryType, reason string) ([]CancelledAgentRun, error)
}

func StartedByAnAgent(startedBy string) bool {
	return strings.HasPrefix(startedBy, startedByAgent)
}

func (r *AgentCallRun) AwaitsCustomerReply() bool {
	return r.Status == RunStatusWaiting && (r.WaitReason == WaitReasonReply || r.WaitReason == WaitReasonForm)
}

func AgentCallContextLine(run *AgentCallRun, now time.Time) string {
	if run == nil {
		return ""
	}
	name := fmt.Sprintf("Fluxo %q", run.WorkflowName)
	switch run.Status {
	case RunStatusRunning:
		return fmt.Sprintf("%s em execução há %s, etapa: %s. Não afirme o resultado antes de ele chegar.",
			name, ElapsedPT(now.Sub(run.StartedAt)), run.stepName())
	case RunStatusWaiting:
		if run.AwaitsCustomerReply() {
			if question := cut(run.LastQuestion, maxQuestionRunes); question != "" {
				return fmt.Sprintf("%s aguardando a resposta do cliente a: %q", name, question)
			}
			return name + " aguardando a resposta do cliente."
		}
		line := fmt.Sprintf("%s em execução há %s, aguardando na etapa: %s", name, ElapsedPT(now.Sub(run.StartedAt)), run.stepName())
		if run.WakeAt != nil {
			line += fmt.Sprintf("; retoma em %s", ElapsedPT(time.UnixMilli(*run.WakeAt).Sub(now)))
		}
		return line + ". Não afirme o resultado antes de ele chegar."
	case RunStatusCompleted:
		if len(run.AgentResult) == 0 {
			return fmt.Sprintf("%s concluído há %s, sem resultado para você; o próprio fluxo cuidou da conversa.", name, run.finishedAgo(now))
		}
		return fmt.Sprintf("%s concluído há %s. %s", name, run.finishedAgo(now), run.resultText())
	case RunStatusError:
		line := fmt.Sprintf("%s falhou: %s.", name, cut(run.Error, maxAgentErrorRunes))
		if len(run.AgentResult) > 0 {
			line += " " + run.resultText()
		}
		return line + " Não prometa o resultado; diga que vai verificar."
	case RunStatusCancelled:
		line := fmt.Sprintf("%s cancelado há %s", name, run.finishedAgo(now))
		if reason := cut(run.Error, maxAgentErrorRunes); reason != "" {
			line += ": " + reason
		}
		return line + "."
	}
	return ""
}

func (r *AgentCallRun) stepName() string {
	if name := strings.TrimSpace(r.CurrentStepName); name != "" {
		return name
	}
	return "etapa atual"
}

func (r *AgentCallRun) finishedAgo(now time.Time) string {
	if r.FinishedAt == nil {
		return ElapsedPT(0)
	}
	return ElapsedPT(now.Sub(*r.FinishedAt))
}

func (r *AgentCallRun) resultText() string {
	encoded, err := json.Marshal(r.AgentResult)
	if err != nil {
		return ""
	}
	return "Resultado do fluxo: " + cut(string(encoded), maxAgentResultRunes)
}

func cut(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func ElapsedPT(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "menos de 1 min"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	case d < 24*time.Hour:
		hours, minutes := int(d/time.Hour), int((d%time.Hour)/time.Minute)
		if minutes == 0 {
			return fmt.Sprintf("%d h", hours)
		}
		return fmt.Sprintf("%d h %d min", hours, minutes)
	}
	days := int(d / (24 * time.Hour))
	if days == 1 {
		return "1 dia"
	}
	return fmt.Sprintf("%d dias", days)
}
