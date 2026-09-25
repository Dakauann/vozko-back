package copilottools

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type MessageBroadcaster interface {
	BroadcastNewMessage(entryID, entryType string, message *conversation.Message)
	BroadcastEntryUpdate(entryID, entryType string, message *conversation.Message)
}

type ConversationActionDeps struct {
	Send      conversation.PersonSendUseCase
	Scheduler sm.PersonSchedulerUseCase
	Entries   conversation.EntryLookup
	Broadcast MessageBroadcaster
}

func conversationSendMeta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceConversations, Action: workspace.ActionSend}
}

type sendMessageArgs struct {
	EntryID   string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	Text      string `json:"text" req:"true" desc:"o texto exato que o cliente vai receber"`
}

type sendMessageTool struct{ deps ConversationActionDeps }

func NewSendMessageTool(deps ConversationActionDeps) copilot.Tool {
	return &sendMessageTool{deps: deps}
}

func (t *sendMessageTool) Meta() copilot.Meta { return conversationSendMeta() }

func (t *sendMessageTool) Definition() tools.Definition {
	return definition("send_message",
		"Envia uma mensagem de texto na conversa, em nome do usuário. Só depois da aprovação do usuário. No WhatsApp oficial só "+
			"funciona com a janela de 24h aberta; fechada, é preciso um modelo aprovado.", sendMessageArgs{})
}

func (t *sendMessageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a sendMessageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
	}
}

func (t *sendMessageTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a sendMessageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	message, err := t.deps.Send.Execute(ctx, personOf(cc), conversation.OperatorSendInput{
		EntryID: target.EntryID, EntryType: string(target.EntryType), WorkspaceID: cc.WorkspaceID, Text: a.Text,
	})
	if err != nil {
		return sendFailure("send_message", err)
	}
	if t.deps.Broadcast != nil {
		t.deps.Broadcast.BroadcastNewMessage(target.EntryID, string(target.EntryType), message)
		t.deps.Broadcast.BroadcastEntryUpdate(target.EntryID, string(target.EntryType), message)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"sent": true}}
}

func sendFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, conversation.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa"}
	case errors.Is(err, conversation.ErrWindowClosed), errors.Is(err, conversation.ErrOutboundWindowClosed):
		return copilot.Result{Status: copilot.StatusError, Message: "a janela de 24h desta conversa está fechada; só um modelo aprovado (list_templates) reabre a conversa"}
	case errors.Is(err, balance.ErrInsufficientBalance):
		return copilot.Result{Status: copilot.StatusError, Message: "saldo insuficiente para enviar; o usuário precisa recarregar"}
	case errors.Is(err, conversation.ErrEntryTypeInvalid), errors.Is(err, conversation.ErrEntryIDRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "conversa desconhecida; use os ids de search_conversations"}
	case errors.Is(err, conversation.ErrMessageContentRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "a mensagem está vazia"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao enviar"}
}

type scheduleMessageArgs struct {
	EntryID     string `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType   string `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	Text        string `json:"text" req:"true" desc:"o texto exato que o cliente vai receber"`
	ScheduledAt string `json:"scheduled_at" req:"true" desc:"data e hora com fuso, RFC3339 (ex.: 2026-09-26T09:00:00-03:00)"`
}

type scheduleMessageTool struct{ deps ConversationActionDeps }

func NewScheduleMessageTool(deps ConversationActionDeps) copilot.Tool {
	return &scheduleMessageTool{deps: deps}
}

func (t *scheduleMessageTool) Meta() copilot.Meta { return conversationSendMeta() }

func (t *scheduleMessageTool) Definition() tools.Definition {
	return definition("schedule_message",
		"Agenda uma mensagem de texto para ser enviada na conversa num horário, em nome do usuário. Só depois da aprovação "+
			"do usuário. No WhatsApp oficial o horário precisa cair dentro da janela de 24h.", scheduleMessageArgs{})
}

func (t *scheduleMessageTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a scheduleMessageArgs
	bindArgs(args, &a)
	return []copilot.Field{
		{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)},
		{Key: "scheduledAt", Value: strings.TrimSpace(a.ScheduledAt)},
	}
}

func (t *scheduleMessageTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a scheduleMessageArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(a.ScheduledAt))
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "scheduled_at deve ter data, hora e fuso (ex.: 2026-09-26T09:00:00-03:00)"}
	}
	out, err := t.deps.Scheduler.Schedule(ctx, personOf(cc), sm.ScheduleInput{
		WorkspaceID: cc.WorkspaceID, EntryID: target.EntryID, EntryType: string(target.EntryType), Text: a.Text, ScheduledAt: at,
	})
	if err != nil {
		return scheduleFailure("schedule_message", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"scheduled_message_id": out.Message.ID, "scheduled_at": out.Message.ScheduledAt.UTC().Format(time.RFC3339),
	}}
}

type cancelScheduledArgs struct {
	ScheduledMessageID string `json:"scheduled_message_id" req:"true" desc:"id da mensagem agendada" id:"true"`
}

type cancelScheduledTool struct{ deps ConversationActionDeps }

func NewCancelScheduledMessageTool(deps ConversationActionDeps) copilot.Tool {
	return &cancelScheduledTool{deps: deps}
}

func (t *cancelScheduledTool) Meta() copilot.Meta { return conversationSendMeta() }

func (t *cancelScheduledTool) Definition() tools.Definition {
	return definition("cancel_scheduled_message", "Cancela uma mensagem agendada que ainda não foi enviada. Só depois da aprovação do usuário.", cancelScheduledArgs{})
}

func (t *cancelScheduledTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a cancelScheduledArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	id, err := knownID(a.ScheduledMessageID, "scheduled_message_id", "schedule_message")
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.Scheduler.Cancel(ctx, personOf(cc), cc.WorkspaceID, id); err != nil {
		return scheduleFailure("cancel_scheduled_message", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"cancelled": true}}
}

func scheduleFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, sm.ErrEntryAccess):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa"}
	case errors.Is(err, sm.ErrWindowClosed):
		return copilot.Result{Status: copilot.StatusError, Message: "a janela de 24h está fechada; não dá para agendar texto livre agora"}
	case errors.Is(err, sm.ErrScheduledAtPastWindow):
		return copilot.Result{Status: copilot.StatusError, Message: "esse horário cai depois que a janela de 24h fecha; escolha um horário antes"}
	case errors.Is(err, sm.ErrScheduledAtTooSoon):
		return copilot.Result{Status: copilot.StatusError, Message: "horário próximo demais de agora; envie já com send_message ou escolha mais tarde"}
	case errors.Is(err, sm.ErrScheduledAtTooFar):
		return copilot.Result{Status: copilot.StatusError, Message: "horário longe demais no futuro"}
	case errors.Is(err, sm.ErrNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: "mensagem agendada desconhecida"}
	case errors.Is(err, sm.ErrNotPending):
		return copilot.Result{Status: copilot.StatusError, Message: "essa mensagem já foi enviada ou cancelada"}
	case errors.Is(err, sm.ErrContentRequired):
		return copilot.Result{Status: copilot.StatusError, Message: "a mensagem está vazia"}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao agendar"}
}

func (t *sendMessageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[sendMessageArgs](t.deps.Entries, cc, args)
	return err
}

func (t *scheduleMessageTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[scheduleMessageArgs](t.deps.Entries, cc, args)
	return err
}

func (t *cancelScheduledTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[cancelScheduledArgs](t.deps.Entries, cc, args)
	return err
}

func messagePreview(text, channel, scheduledAt string) *copilot.Preview {
	return &copilot.Preview{Kind: copilot.PreviewMessage, Data: MessagePreview{
		Text:        strings.TrimSpace(text),
		Channel:     strings.TrimSpace(channel),
		ScheduledAt: strings.TrimSpace(scheduledAt),
	}}
}

func (t *sendMessageTool) Preview(_ context.Context, _ copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a sendMessageArgs
	bindArgs(args, &a)
	return messagePreview(a.Text, a.EntryType, "")
}

func (t *scheduleMessageTool) Preview(_ context.Context, _ copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a scheduleMessageArgs
	bindArgs(args, &a)
	return messagePreview(a.Text, a.EntryType, a.ScheduledAt)
}
