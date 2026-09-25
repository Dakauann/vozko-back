package copilot_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"
	"vozko/domain/readiness"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
	"vozko/domain/media"
	"vozko/usecases/agentloop"
)

const (
	maxHistoryMessages  = 40
	maxTitleLen         = 60
	defaultCopilotModel = "anthropic/claude-sonnet-4"
)

var (
	ErrEmptyMessage   = errors.New("copilot: empty message")
	ErrActionNotFound = errors.New("copilot: pending action not found")
)

type Service struct {
	engine   agentloop.Engine
	registry *Registry
	access   AccessChecker
	funds    FundsChecker
	threads  aichat.ThreadRepository
	messages aichat.MessageRepository
	files    AttachmentResolver
	state    readiness.SnapshotUseCase
	newID    IDGenerator
}

type AttachmentResolver interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

func NewService(
	engine agentloop.Engine,
	reg *Registry,
	access AccessChecker,
	funds FundsChecker,
	threads aichat.ThreadRepository,
	messages aichat.MessageRepository,
	files AttachmentResolver,
	state readiness.SnapshotUseCase,
	newID IDGenerator,
) *Service {
	return &Service{engine: engine, registry: reg, access: access, funds: funds, threads: threads, messages: messages, files: files, state: state, newID: newID}
}

func (s *Service) Stream(ctx context.Context, thread *aichat.Thread, msg copilot.UserMessage, cc copilot.Context, emit agentloop.Emit) error {
	content := strings.TrimSpace(msg.Content)
	if content == "" && len(msg.AttachmentIDs) == 0 {
		return ErrEmptyMessage
	}
	attachments, err := s.resolveAttachments(cc.WorkspaceID, msg.AttachmentIDs)
	if err != nil {
		return err
	}
	if err := s.messages.ExpireProposals(thread.ID); err != nil {
		return err
	}
	return s.runTurn(ctx, thread, content, cc, emit, attachments, true)
}

func (s *Service) resolveAttachments(workspaceID string, ids []string) ([]copilot.Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > copilot.MaxAttachments {
		return nil, copilot.ErrTooManyAttachments
	}
	if s.files == nil {
		return nil, copilot.ErrAttachmentNotFound
	}
	out := make([]copilot.Attachment, 0, len(ids))
	for _, id := range ids {
		m, err := s.files.GetMedia(workspaceID, strings.TrimSpace(id))
		if err != nil || m == nil {
			return nil, copilot.ErrAttachmentNotFound
		}
		out = append(out, copilot.Attachment{MediaID: m.ID, Name: m.DisplayName(), Kind: string(m.Type)})
	}
	return out, nil
}

func (s *Service) runTurn(ctx context.Context, thread *aichat.Thread, content string, cc copilot.Context, emit agentloop.Emit, attachments []copilot.Attachment, persistUserMsg bool, prelude ...toolStep) error {
	prompt := copilot.PromptWithAttachments(content, attachments)
	model := thread.Model
	if strings.TrimSpace(model) == "" {
		model = defaultCopilotModel
	}

	history, err := s.buildHistory(thread.ID)
	if err != nil {
		return err
	}
	if persistUserMsg {
		user := &aichat.Message{ThreadID: thread.ID, Role: aichat.RoleUser, Content: content}
		if len(attachments) > 0 {
			user.Attachments, _ = json.Marshal(attachments)
		}
		if err := s.messages.Create(user); err != nil {
			return err
		}
	}

	rec := &turnRecorder{emit: emit}
	for _, ts := range prelude {
		rec.emitFn("tool", ts.payload())
	}
	cc.Datasets = copilot.NewDatasetStore()

	driver := NewDriver(cc, model, s.registry, s.access, s.funds, s.newID)
	driver.state = s.workspaceState(ctx, cc)
	sess := &agentloop.Session{History: history}
	out := s.engine.Run(ctx, rec.emitFn, driver, DefaultConfig(cc, AnswerTokenBudget), sess, prompt)

	switch out.Kind {
	case agentloop.OutcomePaused:
		pa, _ := out.Pause.Payload.(copilot.PendingAction)
		proposal := lastAssistantContent(sess.History)
		if proposal == "" {
			proposal = "Proponho uma ação que precisa da sua aprovação."
		}
		m := rec.message(thread.ID, proposal, model)
		if err := attachProposal(m, pa); err != nil {
			return err
		}
		if err := s.messages.Create(m); err != nil {
			return err
		}
		emit("awaiting_approval", map[string]interface{}{"actionId": pa.ID, "tool": pa.ToolName, "summary": pa.Summary})
	default:
		reply := lastAssistantContent(sess.History)
		if reply != "" {
			_ = s.messages.Create(rec.message(thread.ID, reply, model))
		}
		if msg := haltMessage(out.Halt); msg != "" {
			emit("error", map[string]interface{}{"error": msg})
		}
		emit("done", map[string]interface{}{"content": reply})
	}

	_ = s.threads.Touch(thread.ID, time.Now().UTC(), model)
	if persistUserMsg && strings.TrimSpace(thread.Title) == "" {
		_ = s.threads.Rename(thread.ID, deriveTitle(titleSource(content, attachments)))
	}
	return nil
}

func (s *Service) workspaceState(ctx context.Context, cc copilot.Context) *readiness.Snapshot {
	if s.state == nil {
		return nil
	}
	snap, err := s.state.Snapshot(ctx, readiness.Person{WorkspaceID: cc.WorkspaceID, UserID: cc.UserID, SystemAdmin: cc.SystemAdmin})
	if err != nil {
		log.Printf("[copilot] workspace state unavailable: %v", err)
		return nil
	}
	return snap
}

func attachProposal(m *aichat.Message, pa copilot.PendingAction) error {
	raw, err := json.Marshal(pa)
	if err != nil {
		return err
	}
	m.ProposalID, m.Proposal, m.ProposalStatus = pa.ID, raw, aichat.ProposalPending
	return nil
}

func (s *Service) claim(threadID, actionID string, outcome aichat.ProposalStatus) (copilot.PendingAction, error) {
	var pa copilot.PendingAction
	m, err := s.messages.ClaimProposal(threadID, actionID, outcome)
	if errors.Is(err, aichat.ErrProposalNotPending) {
		return pa, ErrActionNotFound
	}
	if err != nil {
		return pa, err
	}
	if err := json.Unmarshal(m.Proposal, &pa); err != nil {
		return pa, err
	}
	return pa, nil
}

func (s *Service) Approve(ctx context.Context, thread *aichat.Thread, actionID string, cc copilot.Context, emit agentloop.Emit) error {
	pa, err := s.claim(thread.ID, actionID, aichat.ProposalApproved)
	if err != nil {
		return err
	}
	model := thread.Model
	if strings.TrimSpace(model) == "" {
		model = defaultCopilotModel
	}
	driver := NewDriver(cc, model, s.registry, s.access, s.funds, s.newID)
	res := driver.ExecuteApproved(ctx, pa)
	executed := toolStep{Name: pa.ToolName, Summary: string(res.Status), Ok: res.Status == copilot.StatusOK}
	return s.runTurn(ctx, thread, approvalContinuationPrompt(pa, res), cc, emit, nil, false, executed)
}

func (s *Service) Reject(ctx context.Context, thread *aichat.Thread, actionID string, emit agentloop.Emit) error {
	pa, err := s.claim(thread.ID, actionID, aichat.ProposalRejected)
	if err != nil {
		return err
	}
	content := "Ação cancelada pelo usuário: " + pa.ToolName
	_ = s.messages.Create(&aichat.Message{ThreadID: thread.ID, Role: aichat.RoleAssistant, Content: content, Model: thread.Model})
	emit("done", map[string]interface{}{"content": content, "status": "rejected"})
	return nil
}

func (s *Service) buildHistory(threadID string) ([]ai.Message, error) {
	_, total, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: 1})
	if err != nil {
		return nil, err
	}
	offset := 0
	if total > int64(maxHistoryMessages) {
		offset = int(total) - maxHistoryMessages
	}
	msgs, _, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: maxHistoryMessages, Offset: offset})
	if err != nil {
		return nil, err
	}
	out := make([]ai.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case aichat.RoleAssistant:
			out = append(out, ai.Message{Role: ai.RoleAssistant, Content: m.Content})
		case aichat.RoleSystem:
			out = append(out, ai.Message{Role: ai.RoleSystem, Content: m.Content})
		case aichat.RoleUser:
			out = append(out, ai.Message{Role: ai.RoleUser, Content: copilot.PromptWithAttachments(m.Content, storedAttachments(m.Attachments))})
		}
	}
	return out, nil
}

func storedAttachments(raw []byte) []copilot.Attachment {
	if len(raw) == 0 {
		return nil
	}
	var out []copilot.Attachment
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func titleSource(content string, attachments []copilot.Attachment) string {
	if content != "" || len(attachments) == 0 {
		return content
	}
	return attachments[0].Name
}

func lastAssistantContent(h []ai.Message) string {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == ai.RoleAssistant && strings.TrimSpace(h[i].Content) != "" {
			return h[i].Content
		}
	}
	return ""
}

func approvalContinuationPrompt(pa copilot.PendingAction, res copilot.Result) string {
	var b strings.Builder
	b.WriteString("[SISTEMA] O usuário aprovou a ação que você propôs: ")
	b.WriteString(pa.ToolName)
	if pa.Summary != "" {
		b.WriteString(", ")
		b.WriteString(pa.Summary)
	}
	b.WriteString(". ")
	switch res.Status {
	case copilot.StatusOK:
		b.WriteString("Ela foi executada com SUCESSO. Confirme o resultado ao usuário de forma breve e ofereça o próximo passo, se houver.")
		if d := renderData(res.Data); d != "" {
			b.WriteString(" Dados retornados: ")
			b.WriteString(d)
		}
	case copilot.StatusDenied:
		b.WriteString("Mas você NÃO tem permissão para executá-la. Explique isso ao usuário.")
	default:
		b.WriteString("Mas ela FALHOU com o erro: \"")
		b.WriteString(res.Message)
		b.WriteString("\". Analise a causa, corrija (por exemplo, peça ou defina os campos que faltam) e proponha a ação novamente; se não for possível, explique ao usuário.")
	}
	return b.String()
}

func renderData(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func deriveTitle(firstMessage string) string {
	title := strings.TrimSpace(strings.ReplaceAll(firstMessage, "\n", " "))
	if len(title) > maxTitleLen {
		title = strings.TrimSpace(title[:maxTitleLen]) + "…"
	}
	return title
}

type toolStep struct {
	Name    string              `json:"name"`
	Summary string              `json:"summary"`
	Ok      bool                `json:"ok"`
	Chart   *copilot.Chart      `json:"chart,omitempty"`
	Card    *copilot.ActionCard `json:"card,omitempty"`
}

func (t toolStep) payload() map[string]interface{} {
	return map[string]interface{}{"name": t.Name, "summary": t.Summary, "ok": t.Ok}
}

type turnRecorder struct {
	emit      agentloop.Emit
	reasoning strings.Builder
	tools     []toolStep
}

func (r *turnRecorder) emitFn(eventType string, payload interface{}) {
	switch eventType {
	case "reasoning_delta":
		r.reasoning.WriteString(eventText(payload))
	case "tool":
		r.tools = append(r.tools, toolStepFromPayload(payload))
	case EventChart:
		if chart, ok := payload.(*copilot.Chart); ok && len(r.tools) > 0 {
			r.tools[len(r.tools)-1].Chart = chart
		}
	case EventCard:
		if card, ok := payload.(*copilot.ActionCard); ok && len(r.tools) > 0 {
			r.tools[len(r.tools)-1].Card = card
		}
	}
	r.emit(eventType, payload)
}

func (r *turnRecorder) message(threadID, content, model string) *aichat.Message {
	m := &aichat.Message{ThreadID: threadID, Role: aichat.RoleAssistant, Content: content, Model: model}
	if r.reasoning.Len() > 0 {
		if b, err := json.Marshal(r.reasoning.String()); err == nil {
			m.Reasoning = b
		}
	}
	if len(r.tools) > 0 {
		if b, err := json.Marshal(r.tools); err == nil {
			m.ToolCalls = b
		}
	}
	return m
}

func eventText(payload interface{}) string {
	switch p := payload.(type) {
	case map[string]string:
		return p["text"]
	case map[string]interface{}:
		if s, ok := p["text"].(string); ok {
			return s
		}
	}
	return ""
}

func toolStepFromPayload(payload interface{}) toolStep {
	p, _ := payload.(map[string]interface{})
	name, _ := p["name"].(string)
	summary, _ := p["summary"].(string)
	ok, _ := p["ok"].(bool)
	return toolStep{Name: name, Summary: summary, Ok: ok}
}
