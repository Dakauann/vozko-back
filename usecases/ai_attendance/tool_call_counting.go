package ai_attendance_usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/ai"
	aa "vozko/domain/ai_attendance"
	"vozko/domain/conversation"
	"vozko/domain/metrics"
	"vozko/domain/redact"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/tracepayload"
)

const unlistedToolLabel = "unlisted"

const maxToolErrorBytes = 4 * 1024

var toolCallRedactor = redact.New()

type toolCallCountingAI struct {
	ai.Service
	sessions aa.ToolCallRecorder
	calls    aa.ToolCallLog
	metrics  metrics.AIToolCallMetricsRecorder
	dispatch func(func())
}

func CountToolCalls(inner ai.Service, sessions aa.ToolCallRecorder, calls aa.ToolCallLog, recorder metrics.AIToolCallMetricsRecorder) ai.Service {
	return countToolCalls(inner, sessions, calls, recorder, func(fn func()) { go fn() })
}

func countToolCalls(inner ai.Service, sessions aa.ToolCallRecorder, calls aa.ToolCallLog, recorder metrics.AIToolCallMetricsRecorder, dispatch func(func())) ai.Service {
	if calls == nil || recorder == nil {
		panic("ai attendance: counting tool calls needs the tool call log and the metrics recorder")
	}
	return &toolCallCountingAI{Service: inner, sessions: sessions, calls: calls, metrics: recorder, dispatch: dispatch}
}

func (c *toolCallCountingAI) Generate(ctx context.Context, input ai.GenerateInput) (*ai.GenerateOutput, error) {
	out, err := c.Service.Generate(ctx, input)
	if err != nil || out == nil || len(out.ToolCalls) == 0 {
		return out, err
	}
	ai.EnsureToolCallIDs(out.ToolCalls)
	turn := input.AgentTurn
	if input.WorkspaceID == "" || !turn.Identified() {
		return out, err
	}
	if c.sessions != nil {
		c.sessions.RecordToolCalls(aa.StartInput{
			WorkspaceID: input.WorkspaceID,
			EntryID:     turn.EntryID,
			EntryType:   turn.EntryType,
			AgentID:     turn.AgentID,
			Channel:     turn.EntryType,
			CampaignID:  turn.CampaignID,
			Model:       input.Model,
		}, len(out.ToolCalls))
	}
	c.record(input, out.ToolCalls)
	return out, err
}

func (c *toolCallCountingAI) record(input ai.GenerateInput, calls []ai.ToolCall) {
	offered := offeredTools(input.Tools)
	now := time.Now().UTC()
	records := make([]aa.ToolCall, 0, len(calls))
	for _, call := range calls {
		outcome, failure := toolCallOutcome(call)
		label, isOffered := offered[strings.ToLower(call.Name)]
		if !isOffered {
			label = unlistedToolLabel
		}
		c.metrics.IncAIToolCall(label, string(outcome))
		records = append(records, aa.ToolCall{
			ID:          uuid.New().String(),
			ToolCallID:  call.ID,
			WorkspaceID: input.WorkspaceID,
			EntryID:     input.AgentTurn.EntryID,
			EntryType:   input.AgentTurn.EntryType,
			AgentID:     input.AgentTurn.AgentID,
			SessionID:   input.SessionID,
			ToolName:    call.Name,
			Args:        tracepayload.Map(toolCallRedactor.Map(call.Arguments)),
			Outcome:     outcome,
			Error:       tracepayload.MarkedTextWithin(toolCallRedactor.String(failure), maxToolErrorBytes),
			ErrorCode:   errorCodeOf(call),
			DurationMs:  call.DurationMs,
			RunID:       runStartedBy(call),
			CreatedAt:   now,
		})
	}
	c.dispatch(func() {
		if err := c.calls.Record(records); err != nil {
			slog.Error("ai_attendance.tool_calls.record_failed",
				slog.String("workspace_id", input.WorkspaceID),
				slog.String("entry_id", input.AgentTurn.EntryID),
				slog.String("agent_id", input.AgentTurn.AgentID),
				slog.Int("calls", len(records)),
				slog.String("error", err.Error()))
		}
	})
}

func offeredTools(defs []tools.Definition) map[string]string {
	offered := make(map[string]string, len(defs))
	for _, def := range defs {
		label := def.Name
		if def.HandlerName != "" {
			label = def.HandlerName
		}
		offered[strings.ToLower(def.Name)] = label
	}
	return offered
}

func runStartedBy(call ai.ToolCall) string {
	if call.Result == nil {
		return ""
	}
	return call.Result.RunID
}

func errorCodeOf(call ai.ToolCall) string {
	if call.Result == nil || !call.Result.IsError {
		return ""
	}
	return call.Result.ErrorCode
}

func toolCallOutcome(call ai.ToolCall) (aa.ToolCallOutcome, string) {
	if call.Result == nil {
		return aa.ToolCallRequested, ""
	}
	if !call.Result.IsError {
		return aa.ToolCallOK, ""
	}
	if text, ok := call.Result.Result.(string); ok && text != "" {
		return aa.ToolCallError, text
	}
	if call.Result.ContextUpdateText != "" {
		return aa.ToolCallError, call.Result.ContextUpdateText
	}
	encoded, _ := json.Marshal(call.Result.Result)
	return aa.ToolCallError, string(encoded)
}

var ErrConversationNotVisible = conversation.ErrNotVisible

type ListToolCallsUseCase struct {
	calls      aa.ToolCallLog
	visibility conversation.Visibility
}

func NewListToolCallsUseCase(calls aa.ToolCallLog, visibility conversation.Visibility) *ListToolCallsUseCase {
	return &ListToolCallsUseCase{calls: calls, visibility: visibility}
}

func (uc *ListToolCallsUseCase) Execute(person shared.Person, workspaceID, entryType, entryID string, pagination shared.Pagination) (*shared.PaginatedResult[*aa.ToolCall], error) {
	if err := conversation.RequireVisible(uc.visibility, person, workspaceID, entryID, entryType); err != nil {
		if err != ErrConversationNotVisible {
			slog.Warn("ai_attendance.tool_calls.visibility_unreadable",
				slog.String("workspace_id", workspaceID), slog.String("entry_id", entryID), slog.String("error", err.Error()))
		}
		return nil, err
	}
	return uc.calls.ListByEntry(workspaceID, entryID, entryType, pagination)
}
