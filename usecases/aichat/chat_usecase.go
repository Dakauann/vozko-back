package aichat_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/workspace/workspace_plan"
)

const (
	maxHistoryMessages = 40
	defaultChatTemp    = 0.7
	maxTitleLen        = 60
	chatSystemPrompt   = "Você é um assistente de IA útil, preciso e direto. Responda no idioma do usuário e use Markdown quando ajudar a legibilidade."
)

var (
	ErrForbidden           = errors.New("aichat: forbidden")
	ErrNoSubscription      = errors.New("aichat: active subscription required")
	ErrInsufficientBalance = errors.New("aichat: insufficient balance")
	ErrEmptyMessage        = errors.New("aichat: empty message")
)

type subscriptionReader interface {
	GetCurrentByWorkspaceID(workspaceID string, at time.Time) (*workspace_plan.WorkspaceSubscription, error)
}

type Service struct {
	threads  aichat.ThreadRepository
	messages aichat.MessageRepository
	ai       ai.Service
	funds    *FundsGate
}

func NewService(
	threads aichat.ThreadRepository,
	messages aichat.MessageRepository,
	aiSvc ai.Service,
	funds *FundsGate,
) *Service {
	return &Service{threads: threads, messages: messages, ai: aiSvc, funds: funds}
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

func (s *Service) Precheck(workspaceID, userID, threadID string) (*aichat.Thread, error) {
	thread, err := s.authorizeThread(workspaceID, userID, threadID)
	if err != nil {
		return nil, err
	}
	if err := s.funds.Check(workspaceID); err != nil {
		return nil, err
	}
	return thread, nil
}

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

func deriveTitle(firstMessage string) string {
	title := strings.TrimSpace(strings.ReplaceAll(firstMessage, "\n", " "))
	if len(title) > maxTitleLen {
		title = strings.TrimSpace(title[:maxTitleLen]) + "…"
	}
	return title
}
