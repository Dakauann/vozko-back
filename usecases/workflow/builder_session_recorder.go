package workflow_usecase

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
)

const builderSessionTitleMaxLen = 80

type BuilderSessionRecorder struct {
	repo workflow.BuilderSessionRepository

	mu            sync.Mutex
	session       *workflow.BuilderSession
	assistant     strings.Builder
	assistantOpen bool
	created       bool
	ended         bool
}

func NewBuilderSessionRecorder(repo workflow.BuilderSessionRepository, workspaceID, workflowID string) *BuilderSessionRecorder {
	if repo == nil {
		return nil
	}
	return &BuilderSessionRecorder{
		repo: repo,
		session: &workflow.BuilderSession{
			ID:          uuid.NewString(),
			WorkspaceID: workspaceID,
			WorkflowID:  workflowID,
			StartedAt:   time.Now().UTC(),
		},
	}
}

func (r *BuilderSessionRecorder) Inbound(raw []byte) {
	if r == nil {
		return
	}
	var frame struct {
		Type string `json:"type"`
		Data struct {
			Text string `json:"text"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &frame) != nil || frame.Type != "user_prompt" {
		return
	}
	text := strings.TrimSpace(frame.Data.Text)
	if text == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	msgs := r.flushAssistantLocked()
	if r.session.Title == "" {
		r.session.Title = truncateTitle(text)
	}
	msgs = append(msgs, message(workflow.BuilderMessageRoleUser, text, "", nil))
	r.recordLocked(msgs)
}

func (r *BuilderSessionRecorder) Outbound(raw []byte) {
	if r == nil {
		return
	}
	var frame struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return
	}

	switch frame.Type {
	case "builder_ready":
		var p struct {
			Mode  string `json:"mode"`
			Model string `json:"model"`
		}
		_ = json.Unmarshal(frame.Payload, &p)
		r.mu.Lock()
		r.session.Mode = p.Mode
		r.session.Model = p.Model
		r.mu.Unlock()

	case "assistant_delta":
		var p struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(frame.Payload, &p) == nil && p.Text != "" {
			r.mu.Lock()
			r.assistant.WriteString(p.Text)
			r.assistantOpen = true
			r.mu.Unlock()
		}

	case "assistant_done":
		r.mu.Lock()
		r.recordLocked(r.flushAssistantLocked())
		r.mu.Unlock()

	case "tool":
		var p struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Ok      bool   `json:"ok"`
		}
		if json.Unmarshal(frame.Payload, &p) != nil {
			return
		}
		ok := p.Ok
		r.mu.Lock()
		msgs := r.flushAssistantLocked()
		msgs = append(msgs, message(workflow.BuilderMessageRoleTool, p.Summary, p.Name, &ok))
		r.recordLocked(msgs)
		r.mu.Unlock()

	case "done":
		var p struct {
			Valid   bool   `json:"valid"`
			Summary string `json:"summary"`
		}
		_ = json.Unmarshal(frame.Payload, &p)
		r.mu.Lock()
		msgs := r.flushAssistantLocked()
		if s := strings.TrimSpace(p.Summary); s != "" {
			msgs = append(msgs, message(workflow.BuilderMessageRoleAssistant, s, "", nil))
		}
		r.session.Valid = p.Valid
		r.recordLocked(msgs)
		r.markEndedLocked()
		r.updateSessionLocked()
		r.mu.Unlock()
	}
}

func (r *BuilderSessionRecorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked(r.flushAssistantLocked())
	r.markEndedLocked()
	r.updateSessionLocked()
}

func (r *BuilderSessionRecorder) flushAssistantLocked() []workflow.BuilderMessage {
	if !r.assistantOpen {
		return nil
	}
	text := strings.TrimSpace(r.assistant.String())
	r.assistant.Reset()
	r.assistantOpen = false
	if text == "" {
		return nil
	}
	return []workflow.BuilderMessage{message(workflow.BuilderMessageRoleAssistant, text, "", nil)}
}

func (r *BuilderSessionRecorder) recordLocked(msgs []workflow.BuilderMessage) {
	if len(msgs) == 0 {
		return
	}
	if !r.created {
		snapshot := *r.session
		if err := r.repo.CreateSession(context.Background(), &snapshot); err != nil {
			log.Printf("[builder-session] create failed id=%s: %v", r.session.ID, err)
			return
		}
		r.created = true
	}
	for _, m := range msgs {
		if err := r.repo.AppendMessage(context.Background(), r.session.ID, m); err != nil {
			log.Printf("[builder-session] append failed id=%s: %v", r.session.ID, err)
		}
	}
}

func (r *BuilderSessionRecorder) updateSessionLocked() {
	if !r.created {
		return
	}
	snapshot := *r.session
	if err := r.repo.UpdateSession(context.Background(), &snapshot); err != nil {
		log.Printf("[builder-session] update failed id=%s: %v", r.session.ID, err)
	}
}

func (r *BuilderSessionRecorder) markEndedLocked() {
	if r.ended {
		return
	}
	r.ended = true
	t := time.Now().UTC()
	r.session.EndedAt = &t
}

func message(role workflow.BuilderMessageRole, text, tool string, ok *bool) workflow.BuilderMessage {
	return workflow.BuilderMessage{Role: role, Text: text, Tool: tool, Ok: ok, At: time.Now().UTC()}
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= builderSessionTitleMaxLen {
		return s
	}
	return strings.TrimSpace(s[:builderSessionTitleMaxLen]) + "…"
}

type BuilderSessionService struct {
	repo workflow.BuilderSessionRepository
}

func NewBuilderSessionService(repo workflow.BuilderSessionRepository) *BuilderSessionService {
	return &BuilderSessionService{repo: repo}
}

func (s *BuilderSessionService) ListByWorkflow(ctx context.Context, workspaceID, workflowID string) ([]*workflow.BuilderSession, error) {
	return s.repo.ListByWorkflow(ctx, workspaceID, workflowID)
}

func (s *BuilderSessionService) ListByWorkspace(ctx context.Context, workspaceID string, limit int) ([]*workflow.BuilderSession, error) {
	return s.repo.ListByWorkspace(ctx, workspaceID, limit)
}

func (s *BuilderSessionService) Get(ctx context.Context, workspaceID, id string) (*workflow.BuilderSession, error) {
	return s.repo.GetByID(ctx, workspaceID, id)
}
