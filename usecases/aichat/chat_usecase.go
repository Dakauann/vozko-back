package aichat_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/balance"
	"vozko/domain/workspace/workspace_plan"
)

const (
	// maxHistoryMessages caps how many prior turns we replay to the model, bounding
	// per-turn token cost on long threads.
	maxHistoryMessages = 40
	defaultChatTemp    = 0.7
	maxTitleLen        = 60
	chatSystemPrompt   = "Você é um assistente de IA útil, preciso e direto. Responda no idioma do usuário e use Markdown quando ajudar a legibilidade."
)

var (
	// ErrForbidden means the thread exists but isn't owned by this workspace/user.
	ErrForbidden = errors.New("aichat: forbidden")
	// ErrNoSubscription means the workspace has no active plan (chat requires one).
	ErrNoSubscription = errors.New("aichat: active subscription required")
	// ErrInsufficientBalance means the workspace balance is depleted.
	ErrInsufficientBalance = errors.New("aichat: insufficient balance")
	ErrEmptyMessage        = errors.New("aichat: empty message")
)

// subscriptionReader is the slice of the plan subscription repo we need to gate
// chat on an active plan.
type subscriptionReader interface {
	GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error)
}

// Service orchestrates the in-app AI chat: thread/message persistence plus the
// streamed, billing-safe generation turn. Billing itself is handled by the AI
// service (it publishes ai.billing.completed keyed on the request, idempotently),
// so this layer only gates entry (active plan + positive balance) and persists.
type Service struct {
	threads  aichat.ThreadRepository
	messages aichat.MessageRepository
	ai       ai.Service
	balance  balance.CachedBalanceChecker
	subs     subscriptionReader
}

func NewService(
	threads aichat.ThreadRepository,
	messages aichat.MessageRepository,
	aiSvc ai.Service,
	bal balance.CachedBalanceChecker,
	subs subscriptionReader,
) *Service {
	return &Service{threads: threads, messages: messages, ai: aiSvc, balance: bal, subs: subs}
}

func (s *Service) CreateThread(workspaceID, userID, model, title string) (*aichat.Thread, error) {
	t := &aichat.Thread{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Model:       strings.TrimSpace(model),
		Title:       strings.TrimSpace(title),
	}
	if err := s.threads.Create(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) ListThreads(workspaceID, userID string, limit, offset int) ([]*aichat.Thread, int64, error) {
	return s.threads.ListByUser(aichat.ListThreadsInput{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Limit:       limit,
		Offset:      offset,
	})
}

func (s *Service) ListMessages(workspaceID, userID, threadID string, limit, offset int) ([]*aichat.Message, int64, error) {
	if _, err := s.authorizeThread(workspaceID, userID, threadID); err != nil {
		return nil, 0, err
	}
	return s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: limit, Offset: offset})
}

func (s *Service) RenameThread(workspaceID, userID, threadID, title string) error {
	if _, err := s.authorizeThread(workspaceID, userID, threadID); err != nil {
		return err
	}
	return s.threads.Rename(threadID, strings.TrimSpace(title))
}

func (s *Service) DeleteThread(workspaceID, userID, threadID string) error {
	if _, err := s.authorizeThread(workspaceID, userID, threadID); err != nil {
		return err
	}
	if err := s.messages.DeleteByThread(threadID); err != nil {
		return err
	}
	return s.threads.Delete(threadID)
}

// Precheck authorizes the thread and gates on plan + balance. The SSE handler
// calls this BEFORE writing stream headers so gate failures map to real HTTP
// status codes (403/402) instead of an in-stream error.
func (s *Service) Precheck(workspaceID, userID, threadID string) (*aichat.Thread, error) {
	thread, err := s.authorizeThread(workspaceID, userID, threadID)
	if err != nil {
		return nil, err
	}
	if err := s.gate(workspaceID); err != nil {
		return nil, err
	}
	return thread, nil
}

// Stream persists the user turn, replays bounded history to the model, relays
// every stream event via emit, then persists the assistant turn (even a partial
// one if the client disconnected, the AI service still bills the partial). The
// thread passed in must already be authorized + gated via Precheck.
func (s *Service) Stream(ctx context.Context, thread *aichat.Thread, content, model string, emit func(ai.StreamEvent)) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return ErrEmptyMessage
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = thread.Model
	}

	userMsg := &aichat.Message{ThreadID: thread.ID, Role: aichat.RoleUser, Content: content}
	if err := s.messages.Create(userMsg); err != nil {
		return err
	}

	aiMessages, err := s.buildHistory(thread.ID)
	if err != nil {
		return err
	}

	streamCh, err := s.ai.GenerateStream(ctx, ai.GenerateInput{
		Model:        model,
		SystemPrompt: chatSystemPrompt,
		Messages:     aiMessages,
		Temperature:  defaultChatTemp,
		WorkspaceID:  thread.WorkspaceID,
	})
	if err != nil {
		return err
	}

	var full strings.Builder
	var usage *ai.Usage
	for ev := range streamCh {
		emit(ev)
		switch ev.Type {
		case ai.StreamEventToken:
			full.WriteString(ev.Token)
		case ai.StreamEventDone:
			if ev.FullText != "" {
				full.Reset()
				full.WriteString(ev.FullText)
			}
			usage = ev.Usage
		}
	}

	// Persist whatever the assistant produced (partial included), so the thread is
	// consistent even if the client cancelled mid-stream.
	assistant := &aichat.Message{
		ThreadID: thread.ID,
		Role:     aichat.RoleAssistant,
		Content:  full.String(),
		Model:    model,
	}
	if usage != nil {
		assistant.PromptTokens = usage.PromptTokens
		assistant.CompletionTokens = usage.CompletionTokens
	}
	if assistant.Content != "" {
		if err := s.messages.Create(assistant); err != nil {
			return err
		}
	}

	_ = s.threads.Touch(thread.ID, time.Now().UTC(), model)
	if strings.TrimSpace(thread.Title) == "" {
		_ = s.threads.Rename(thread.ID, deriveTitle(content))
	}
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
	history, _, err := s.messages.ListByThread(aichat.ListMessagesInput{ThreadID: threadID, Limit: maxHistoryMessages, Offset: offset})
	if err != nil {
		return nil, err
	}

	out := make([]ai.Message, 0, len(history))
	for _, m := range history {
		role := ai.RoleUser
		switch m.Role {
		case aichat.RoleAssistant:
			role = ai.RoleAssistant
		case aichat.RoleSystem:
			role = ai.RoleSystem
		case aichat.RoleTool:
			// Tool turns aren't replayed in plain chat v1.
			continue
		}
		out = append(out, ai.Message{Role: role, Content: m.Content})
	}
	return out, nil
}

func (s *Service) authorizeThread(workspaceID, userID, threadID string) (*aichat.Thread, error) {
	t, err := s.threads.GetByID(threadID)
	if err != nil {
		return nil, err
	}
	if t.WorkspaceID != workspaceID || t.UserID != userID {
		return nil, ErrForbidden
	}
	return t, nil
}

// gate fails closed: chat requires an active subscription and a positive balance.
func (s *Service) gate(workspaceID string) error {
	sub, err := s.subs.GetCurrentByWorkspaceID(workspaceID, time.Now().UTC())
	if err != nil || sub == nil {
		return ErrNoSubscription
	}
	bal, err := s.balance.GetBalance(workspaceID)
	if err != nil {
		return err
	}
	// TODO: FIX, this should calculate, the max use of the selected ai model: MaxTokens * PricePerToken, and compare to the balance. For now, just check if the balance is positive.
	if bal <= 0 {
		return ErrInsufficientBalance
	}
	return nil
}

func deriveTitle(firstMessage string) string {
	title := strings.TrimSpace(strings.ReplaceAll(firstMessage, "\n", " "))
	if len(title) > maxTitleLen {
		title = strings.TrimSpace(title[:maxTitleLen]) + "…"
	}
	return title
}
