package buildersession

import (
	"time"

	workflowdomain "vozko/domain/workflow"
)

type ListByWorkflowRequest struct {
	WorkflowID string `json:"workflowId" example:"wf_a1b2c3d4"`
}

type GetSessionRequest struct {
	SessionID string `json:"sessionId" example:"bs_a1b2c3d4"`
}

type BuilderMessageResponse struct {
	Role string    `json:"role" example:"user"`
	Text string    `json:"text" example:"Crie um fluxo de boas-vindas"`
	Tool string    `json:"tool,omitempty" example:"add_node"`
	Ok   *bool     `json:"ok,omitempty" example:"true"`
	At   time.Time `json:"at" example:"2026-07-19T12:00:00Z"`
}

type BuilderSessionResponse struct {
	ID           string                   `json:"id" example:"bs_a1b2c3d4"`
	WorkspaceID  string                   `json:"workspaceId" example:"ws_a1b2c3d4"`
	WorkflowID   string                   `json:"workflowId,omitempty" example:"wf_a1b2c3d4"`
	Mode         string                   `json:"mode" example:"edicao"`
	Model        string                   `json:"model,omitempty"`
	Title        string                   `json:"title" example:"Fluxo de boas-vindas"`
	Messages     []BuilderMessageResponse `json:"messages,omitempty"`
	MessageCount int                      `json:"messageCount" example:"12"`
	Valid        bool                     `json:"valid" example:"true"`
	StartedAt    time.Time                `json:"startedAt" example:"2026-07-19T12:00:00Z"`
	EndedAt      *time.Time               `json:"endedAt,omitempty" example:"2026-07-19T12:05:00Z"`
}

func toBuilderSessionResponses(sessions []*workflowdomain.BuilderSession) []BuilderSessionResponse {
	if sessions == nil {
		return nil
	}
	out := make([]BuilderSessionResponse, len(sessions))
	for i, s := range sessions {
		out[i] = toBuilderSessionResponse(s)
	}
	return out
}

func toBuilderSessionResponse(s *workflowdomain.BuilderSession) BuilderSessionResponse {
	if s == nil {
		return BuilderSessionResponse{}
	}
	return BuilderSessionResponse{
		ID:           s.ID,
		WorkspaceID:  s.WorkspaceID,
		WorkflowID:   s.WorkflowID,
		Mode:         s.Mode,
		Model:        s.Model,
		Title:        s.Title,
		Messages:     toBuilderMessageResponses(s.Messages),
		MessageCount: s.MessageCount,
		Valid:        s.Valid,
		StartedAt:    s.StartedAt,
		EndedAt:      s.EndedAt,
	}
}

func toBuilderMessageResponses(msgs []workflowdomain.BuilderMessage) []BuilderMessageResponse {
	if msgs == nil {
		return nil
	}
	out := make([]BuilderMessageResponse, len(msgs))
	for i, m := range msgs {
		out[i] = BuilderMessageResponse{
			Role: string(m.Role),
			Text: m.Text,
			Tool: m.Tool,
			Ok:   m.Ok,
			At:   m.At,
		}
	}
	return out
}
