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
	screens  copilot.ScreenMailbox
	turns    *TurnRegistry
	queue    aichat.QueueRepository
	adopter  aichat.ProjectAdopter

	retryBase time.Duration
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
	return &Service{engine: engine, registry: reg, access: access, funds: funds, threads: threads, messages: messages, files: files, state: state, newID: newID, turns: NewTurnRegistry(copilot.DefaultTurnLimits())}
}

func (s *Service) Stream(ctx context.Context, thread *aichat.Thread, msg copilot.UserMessage, cc copilot.Context, emit agentloop.Emit) error {
	content, attachments, err := s.prepare(cc.WorkspaceID, msg)
	fromQueue := errors.Is(err, ErrEmptyMessage)
	switch {
	case fromQueue:
		queued, qerr := s.hasQueued(thread.ID)
		if qerr != nil {
			return qerr
		}
		if !queued {
			return ErrEmptyMessage
		}
	case err != nil:
		return err
	}
	if err := s.messages.ExpireProposals(thread.ID); err != nil {
		return err
	}
	return s.runTurn(ctx, thread, turnStart{content: content, attachments: attachments, persist: !fromQueue, fromQueue: fromQueue}, cc, emit)
}

func (s *Service) prepare(workspaceID string, msg copilot.UserMessage) (string, []copilot.Attachment, error) {
	content := strings.TrimSpace(msg.Content)
	if content == "" && len(msg.AttachmentIDs) == 0 {
		return "", nil, ErrEmptyMessage
	}
	attachments, err := s.resolveAttachments(workspaceID, msg.AttachmentIDs)
	if err != nil {
		return "", nil, err
	}
	return content, attachments, nil
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
		out = append(out, copilot.Attachment{MediaID: m.ID, Name: m.DisplayName(), Kind: string(m.Type), URL: m.URL})
	}
	return out, nil
}

type turnStart struct {
	content     string
	attachments []copilot.Attachment
	persist     bool
	fromQueue   bool
	notice      bool
	prelude     []toolStep
	approved    []ai.Message
}

func (s *Service) runTurn(ctx context.Context, thread *aichat.Thread, start turnStart, cc copilot.Context, emit agentloop.Emit) error {
	model := thread.Model
	if strings.TrimSpace(model) == "" {
		model = defaultCopilotModel
	}
	level := ai.ParseReasoningLevel(thread.ReasoningLevel)
	limits, err := ai.ResolveAgentModel(ctx, s.engine.AI, model, level)
	if refusal, refused := ai.RefusalOf(err); refused {
		log.Printf("[copilot] thread %s refused model %s: %v", thread.ID, model, err)
		emit("error", map[string]interface{}{"error": refusal.Message, "code": refusal.Code})
		emit("done", map[string]interface{}{"content": ""})
		return nil
	}
	sees := limits.SeesImages()

	history, err := s.buildHistory(thread.ID, model, sees)
	if err != nil {
		return err
	}
	userTurn := start.persist || start.fromQueue
	if userTurn {
		delivered, err := s.deliverQueued(thread.ID, emit)
		if err != nil {
			return err
		}
		if start.fromQueue {
			if len(delivered) == 0 {
				emit("done", map[string]interface{}{"content": ""})
				return nil
			}
			last := delivered[len(delivered)-1]
			start.content, start.attachments = last.Content, storedAttachments(last.Attachments)
			delivered = delivered[:len(delivered)-1]
		}
		for _, q := range delivered {
			history = append(history, userMessage(q.Message(), agentloop.UserRequest, sees))
		}
	}
	if start.persist {
		user := &aichat.Message{ThreadID: thread.ID, Role: aichat.RoleUser, Content: start.content}
		if len(start.attachments) > 0 {
			user.Attachments, _ = json.Marshal(start.attachments)
		}
		if err := s.messages.Create(user); err != nil {
			return err
		}
	}
	prompt := copilot.PromptWithAttachments(start.content, start.attachments, sees)

	rec := &turnRecorder{emit: emit}
	for _, ts := range start.prelude {
		emitStep(rec.emitFn, ts)
	}
	cc.Datasets = copilot.NewDatasetStore()
	cc.ChargeReference = aichat.ChargeReference(thread.ID)
	cc.SeesImages = sees
	cc = s.attachScreen(cc, thread.ID, emit)

	driver := NewDriver(cc, model, s.registry, s.access, s.funds, s.newID)
	driver.state = s.workspaceState(ctx, cc)
	driver.reattach = func(next copilot.Context) copilot.Context {
		s.turns.Moved(thread.ID, next.View)
		return s.attachScreen(next, thread.ID, emit)
	}
	driver.adopt = func(projectID string) error { return s.adopt(thread, projectID) }
	driver.checkpoint = s.checkpointer(thread.ID)
	driver.inbox = s.withOpenings(thread.ID, rec, driver, s.inbox(thread.ID, model, sees, rec, emit))
	sess := &agentloop.Session{History: history, Turn: start.approved, PromptImages: copilot.ImageURLs(start.attachments)}
	if start.notice {
		sess.Frame = agentloop.AppNotice
	}
	cfg := DefaultConfig(cc, limits)
	cfg.ReasoningEffort = level
	cfg.ProviderRetryBase = s.retryBase
	out := s.engine.Run(ctx, rec.emitFn, driver, cfg, sess, prompt)

	switch out.Kind {
	case agentloop.OutcomePaused:
		pa, _ := out.Pause.Payload.(copilot.PendingAction)
		proposal := lastAssistantContent(sess.History)
		if proposal == "" {
			proposal = "Proponho uma ação que precisa da sua aprovação."
		}
		m, err := rec.message(thread.ID, proposal, model, sess.Turn)
		if err != nil {
			return err
		}
		if err := attachProposal(m, pa); err != nil {
			return err
		}
		if err := s.messages.Create(m); err != nil {
			return err
		}
		emit("awaiting_approval", map[string]interface{}{"actionId": pa.ID, "tool": pa.ToolName, "summary": pa.Summary})
	default:
		asked := s.askAfterRepeat(out.Halt, sess.History, rec)
		reply := lastAssistantContent(sess.History)
		if reply != "" || len(rec.tools) > 0 {
			m, err := rec.message(thread.ID, reply, model, sess.Turn)
			if err != nil {
				return err
			}
			_ = s.messages.Create(m)
		}
		if failure := haltFailure(out.Halt); failure != nil && !asked {
			emit("error", failure)
		}
		emit("done", map[string]interface{}{"content": reply})
	}

	_ = s.threads.Touch(thread.ID, time.Now().UTC(), model, string(level))
	if userTurn && strings.TrimSpace(thread.Title) == "" {
		_ = s.threads.Rename(thread.ID, deriveTitle(titleSource(start.content, start.attachments)))
	}
	return nil
}

func (s *Service) checkpointer(threadID string) func(text string) error {
	return func(text string) error {
		return s.threads.SaveCheckpoint(threadID, text, time.Now().UTC())
	}
}

func (s *Service) workspaceState(ctx context.Context, cc copilot.Context) *readiness.Snapshot {
	if s.state == nil || cc.View.Focused() {
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

func (s *Service) Approve(ctx context.Context, thread *aichat.Thread, actionID string, approval copilot.Approval, cc copilot.Context, emit agentloop.Emit) error {
	pa, err := s.claim(thread.ID, actionID, aichat.ProposalApproved)
	if err != nil {
		return err
	}
	model := thread.Model
	if strings.TrimSpace(model) == "" {
		model = defaultCopilotModel
	}
	approvedCC := s.attachScreen(cc, thread.ID, emit)
	approvedCC.SeesImages = s.modelLimits(ctx, model).SeesImages()
	approvedCC.ChargeReference = aichat.ChargeReference(thread.ID)
	driver := NewDriver(approvedCC, model, s.registry, s.access, s.funds, s.newID)
	res := driver.ExecuteApproved(ctx, pa, approval, emit)
	executed := stepFromResult(pa.ToolName, res)
	if tool, found := s.registry.Get(pa.ToolName); found && executed.Ok && tool.Meta().Mutating {
		executed.Changed = string(tool.Meta().Resource)
	}
	return s.runTurn(ctx, thread, turnStart{content: approvalContinuationPrompt(pa, res), prelude: []toolStep{executed}, approved: approvedExchange(pa, res)}, cc, emit)
}

func approvedExchange(pa copilot.PendingAction, res copilot.Result) []ai.Message {
	return []ai.Message{
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: pa.ID, Name: pa.ToolName, Arguments: pa.Args}}},
		{Role: ai.RoleTool, ToolCallID: pa.ID, Content: renderResult(res)},
	}
}

const rejectedActionMessage = "Ação cancelada. Nada foi alterado."

func (s *Service) Reject(ctx context.Context, thread *aichat.Thread, actionID string, emit agentloop.Emit) error {
	if _, err := s.claim(thread.ID, actionID, aichat.ProposalRejected); err != nil {
		return err
	}
	content := rejectedActionMessage
	_ = s.messages.Create(&aichat.Message{ThreadID: thread.ID, Role: aichat.RoleAssistant, Content: content, Model: thread.Model})
	emit("done", map[string]interface{}{"content": content, "status": "rejected"})
	return nil
}

func (s *Service) modelLimits(ctx context.Context, model string) ai.ModelInfo {
	m, _ := ai.ResolveAgentModel(ctx, s.engine.AI, model, "")
	return m
}

func (s *Service) buildHistory(threadID, model string, sees bool) ([]ai.Message, error) {
	thread, err := s.threads.GetByID(threadID)
	if err != nil {
		return nil, err
	}
	msgs, _, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, After: thread.CheckpointAt})
	if err != nil {
		return nil, err
	}
	out := make([]ai.Message, 0, len(msgs)+1)
	if thread.Checkpoint != "" {
		out = append(out, ai.Message{Role: ai.RoleUser, Content: thread.Checkpoint})
	}
	for _, m := range msgs {
		switch m.Role {
		case aichat.RoleAssistant:
			turn, err := aichat.DecodeTranscript(m.Transcript)
			if err != nil {
				return nil, err
			}
			if len(turn) > 0 {
				out = append(out, aichat.ReplayFor(turn, m.Model, model)...)
			} else if content := assistantHistoryContent(m); content != "" {
				out = append(out, ai.Message{Role: ai.RoleAssistant, Content: content})
			}
		case aichat.RoleSystem:
			out = append(out, ai.Message{Role: ai.RoleSystem, Content: m.Content})
		case aichat.RoleUser:
			out = append(out, userMessage(m, agentloop.UserRequest, sees))
		}
	}
	return out, nil
}

func userMessage(m *aichat.Message, frame func(string) string, sees bool) ai.Message {
	attachments := storedAttachments(m.Attachments)
	return ai.Message{Role: ai.RoleUser, Content: frame(copilot.PromptWithAttachments(m.Content, attachments, sees)), Images: copilot.ImageURLs(attachments)}
}

func assistantHistoryContent(m *aichat.Message) string {
	notes := toolNotes(m.ToolCalls)
	content := strings.TrimSpace(m.Content)
	switch {
	case notes == "":
		return content
	case content == "":
		return notes
	}
	return content + "\n\n" + notes
}

func toolNotes(raw []byte) string {
	var steps []toolStep
	if len(raw) == 0 || json.Unmarshal(raw, &steps) != nil || len(steps) == 0 {
		return ""
	}
	notes := make([]string, 0, len(steps))
	for _, step := range steps {
		note := step.Name + ": " + step.Summary
		if step.Media != nil {
			note += " (" + string(step.Media.Kind) + " media_id " + step.Media.MediaID + ")"
		}
		if step.Result != "" {
			note += ", retornou " + step.Result
		}
		notes = append(notes, note)
	}
	return "[Ferramentas executadas] " + strings.Join(notes, "; ")
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
	for i := len(h) - 1; i >= 0 && h[i].Role != ai.RoleUser; i-- {
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
	Media   *copilot.Media      `json:"image,omitempty"`
	Subject *copilot.Subject    `json:"subject,omitempty"`
	Result  string              `json:"result,omitempty"`
	Changed string              `json:"changed,omitempty"`
	Error   string              `json:"error,omitempty"`
	Awaits  *copilot.View       `json:"awaits,omitempty"`
}

const maxToolErrorRunes = 500

func stepFromResult(name string, res copilot.Result) toolStep {
	step := toolStep{Name: name, Summary: string(res.Status), Ok: res.Status == copilot.StatusOK, Chart: res.Chart, Card: res.Card, Media: res.Media, Subject: res.Subject, Result: renderData(res.Data)}
	if !step.Ok {
		step.Error = clippedError(res.Message)
	}
	if step.Ok && res.Opening != nil {
		step.Awaits = res.Opening.Awaiting()
	}
	if !step.Ok && res.AwaitsScreen != nil {
		step.Awaits = res.AwaitsScreen
	}
	return step
}

const maxLoggedArgumentRunes = 1500

func failedArguments(tool copilot.Tool, args map[string]interface{}) string {
	logger, ok := tool.(copilot.ArgumentLogger)
	if !ok || !logger.LogsArguments() {
		return ""
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	runes := []rune(string(raw))
	if len(runes) > maxLoggedArgumentRunes {
		runes = runes[:maxLoggedArgumentRunes]
	}
	return string(runes)
}

func clippedError(message string) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) > maxToolErrorRunes {
		runes = runes[:maxToolErrorRunes]
	}
	return string(runes)
}

func emitStep(emit agentloop.Emit, ts toolStep) {
	emit("tool", ts.payload())
	if ts.Chart != nil {
		emit(EventChart, ts.Chart)
	}
	if ts.Card != nil {
		emit(EventCard, ts.Card)
	}
	if ts.Media != nil {
		emit(EventImage, ts.Media)
	}
}

func (t toolStep) payload() map[string]interface{} {
	payload := map[string]interface{}{"name": t.Name, "summary": t.Summary, "ok": t.Ok}
	if t.Changed != "" {
		payload["changed"] = t.Changed
	}
	if t.Subject != nil {
		payload["subject"] = t.Subject
	}
	if t.Error != "" {
		payload["error"] = t.Error
	}
	if t.Awaits != nil {
		payload["awaits"] = t.Awaits
	}
	return payload
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
	case agentloop.EventAssistantRetry:
		if notice, ok := payload.(agentloop.RetryNotice); ok {
			r.takeBackReasoning(notice.Reasoning)
		}
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
	case EventImage:
		if media, ok := payload.(*copilot.Media); ok && len(r.tools) > 0 {
			r.tools[len(r.tools)-1].Media = media
		}
	}
	r.emit(eventType, payload)
}

func (r *turnRecorder) takeBackReasoning(shown string) {
	kept, found := strings.CutSuffix(r.reasoning.String(), shown)
	if shown == "" || !found {
		return
	}
	r.reasoning.Reset()
	r.reasoning.WriteString(kept)
}

func (r *turnRecorder) reset() {
	r.reasoning.Reset()
	r.tools = nil
}

func (r *turnRecorder) awaits(projectID string) bool {
	for _, step := range r.tools {
		if step.Awaits != nil && step.Awaits.ProjectID == projectID {
			return true
		}
	}
	return false
}

func (r *turnRecorder) opened(projectID string) {
	for i := range r.tools {
		if r.tools[i].Awaits != nil && r.tools[i].Awaits.ProjectID == projectID {
			r.tools[i].Awaits = nil
		}
	}
}

func (r *turnRecorder) message(threadID, content, model string, turn []ai.Message) (*aichat.Message, error) {
	transcript, err := aichat.EncodeTranscript(turn)
	if err != nil {
		return nil, err
	}
	m := &aichat.Message{ThreadID: threadID, Role: aichat.RoleAssistant, Content: content, Model: model, Transcript: transcript}
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
	return m, nil
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
	subject, _ := p["subject"].(*copilot.Subject)
	failure, _ := p["error"].(string)
	awaits, _ := p["awaits"].(*copilot.View)
	return toolStep{Name: name, Summary: summary, Ok: ok, Subject: subject, Error: failure, Awaits: awaits}
}
