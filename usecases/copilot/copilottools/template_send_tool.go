package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	"vozko/domain/copilot"
	sm "vozko/domain/scheduled_message"
	"vozko/domain/tools"
	tmpl "vozko/domain/whatsapp/template"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workspace"
)

type TemplateSendDeps struct {
	Scheduler sm.PersonSchedulerUseCase
	Send      conversation.PersonTemplateSendUseCase
	Templates tmpl.WorkspaceTemplatesUseCase
	Costs     tmpl.TemplateCostReader
	Entries   conversation.EntryLookup
}

type sendTemplateArgs struct {
	ScheduledAt string   `json:"scheduled_at" desc:"opcional: agenda em vez de enviar agora; data e hora RFC3339 com fuso"`
	EntryID     string   `json:"entry_id" req:"true" desc:"entry_id exato de search_conversations"`
	EntryType   string   `json:"entry_type" req:"true" desc:"entry_type exato de search_conversations"`
	TemplateID  string   `json:"template_id" req:"true" desc:"template_id exato de list_templates" id:"true"`
	Variables   []string `json:"variables" desc:"valores de {{1}}, {{2}}..., na ordem; exatamente quantos o modelo pede"`
}

type sendTemplateTool struct{ deps TemplateSendDeps }

func NewSendTemplateTool(deps TemplateSendDeps) copilot.Tool { return &sendTemplateTool{deps: deps} }

func (t *sendTemplateTool) Meta() copilot.Meta {
	return copilot.Meta{Mutating: true, Resource: workspace.ResourceConversations, Action: workspace.ActionReopen}
}

func (t *sendTemplateTool) Definition() tools.Definition {
	return definition("send_template",
		"Envia um modelo aprovado do WhatsApp agora ou agenda com scheduled_at, inclusive fora da janela de 24h. É cobrado do saldo no envio: a aprovação mostra o "+
			"custo. Só depois da aprovação do usuário.", sendTemplateArgs{})
}

func (t *sendTemplateTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a sendTemplateArgs
	bindArgs(args, &a)
	fields := []copilot.Field{{Key: "conversation", Value: describeConversation(t.deps.Entries, cc, a.EntryID, a.EntryType)}}
	if a.ScheduledAt != "" {
		fields = append(fields, copilot.Field{Key: "scheduledAt", Value: strings.TrimSpace(a.ScheduledAt)})
	}
	template, err := t.template(cc, a.TemplateID)
	if err != nil {
		return append(fields, copilot.Field{Key: "template", Value: "modelo desconhecido ou sem acesso"})
	}
	fields = append(fields,
		copilot.Field{Key: "template", Value: template.Name},
		copilot.Field{Key: "cost", Value: t.cost(cc, template)},
	)
	return fields
}

func (t *sendTemplateTool) template(cc copilot.Context, raw string) (*tmpl.Template, error) {
	id, err := knownID(raw, "template_id", "list_templates")
	if err != nil {
		return nil, err
	}
	return t.deps.Templates.Get(cc.WorkspaceID, id)
}

func (t *sendTemplateTool) cost(cc copilot.Context, template *tmpl.Template) string {
	category, err := template.BillingCategory()
	if err != nil {
		return "indisponível"
	}
	micros, err := t.deps.Costs.GetTemplateCostMicros(cc.WorkspaceID, category)
	if err != nil || micros <= 0 {
		return "indisponível"
	}
	return fmt.Sprintf("US$ %.4f", float64(micros)/1_000_000)
}

func (t *sendTemplateTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a sendTemplateArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	target, err := targetOf(a.EntryID, a.EntryType)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	template, err := t.template(cc, a.TemplateID)
	if err != nil {
		return templateSendFailure(err)
	}
	if !template.IsReadyToSend() {
		return copilot.Result{Status: copilot.StatusError, Message: "este modelo ainda não pode ser enviado (não aprovado ou sem mídia do cabeçalho)"}
	}
	if want := template.ParameterCount(); len(a.Variables) != want {
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("o modelo pede %d variáveis e vieram %d", want, len(a.Variables))}
	}
	if a.ScheduledAt != "" {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(a.ScheduledAt))
		if err != nil {
			return copilot.Result{Status: copilot.StatusError, Message: "scheduled_at deve ter data, hora e fuso RFC3339"}
		}
		if t.deps.Scheduler == nil {
			return copilot.Result{Status: copilot.StatusError, Message: "agendamento de modelos indisponível"}
		}
		out, err := t.deps.Scheduler.Schedule(ctx, personOf(cc), sm.ScheduleInput{
			WorkspaceID: cc.WorkspaceID, EntryID: target.EntryID, EntryType: string(target.EntryType), ScheduledAt: at,
			Template: &sm.TemplateContent{ID: template.ID, BodyParams: a.Variables},
		})
		if err != nil {
			if errors.Is(err, sm.ErrEntryAccess) || errors.Is(err, sm.ErrTemplatePermission) || errors.Is(err, sm.ErrScheduledAtTooSoon) || errors.Is(err, sm.ErrScheduledAtTooFar) {
				return scheduleFailure("send_template", err)
			}
			return templateSendFailure(err)
		}
		return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
			"scheduled_message_id": out.Message.ID, "scheduled_at": out.Message.ScheduledAt.UTC().Format(time.RFC3339), "template": template.Name,
		}}
	}
	if _, err := t.deps.Send.Execute(personOf(cc), conversation.TemplateSendRequest{
		WorkspaceID: cc.WorkspaceID, EntryID: target.EntryID, EntryType: string(target.EntryType),
		TemplateID: template.ID, Variables: a.Variables,
	}); err != nil {
		return templateSendFailure(err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"sent": true, "template": template.Name}}
}

func templateSendFailure(err error) copilot.Result {
	switch {
	case errors.Is(err, conversation.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não tem acesso a esta conversa"}
	case errors.Is(err, tmpl.ErrTemplateAccessDenied), errors.Is(err, wo.ErrTemplateForbidden):
		return copilot.Result{Status: copilot.StatusDenied, Message: "este workspace não tem acesso a esse modelo"}
	case errors.Is(err, tmpl.ErrTemplateNotFound), errors.Is(err, wo.ErrTemplateNotFound), errors.Is(err, errInvalidArgs):
		return copilot.Result{Status: copilot.StatusError, Message: "modelo desconhecido; use os ids de list_templates"}
	case errors.Is(err, tmpl.ErrTemplatePhoneMismatch):
		return copilot.Result{Status: copilot.StatusError, Message: "esse modelo é de outra conta do WhatsApp, diferente do número desta conversa"}
	case errors.Is(err, balance.ErrInsufficientBalance):
		return copilot.Result{Status: copilot.StatusError, Message: "saldo insuficiente para enviar o modelo; o usuário precisa recarregar"}
	case errors.Is(err, balance.ErrMonthlySendCapReached):
		return copilot.Result{Status: copilot.StatusError, Message: "o limite mensal de envios de modelos deste workspace foi atingido; o usuário precisa falar com a administração"}
	case errors.Is(err, wo.ErrWithinSpamWindow):
		return copilot.Result{Status: copilot.StatusError, Message: "este contato recebeu uma mensagem deste número há pouco tempo; a proteção contra spam do workspace bloqueia um novo modelo agora"}
	case errors.Is(err, wo.ErrLeadBlocked):
		return copilot.Result{Status: copilot.StatusError, Message: "este contato está bloqueado"}
	case errors.Is(err, wo.ErrSendOutcomeUnknown):
		return copilot.Result{Status: copilot.StatusError, Message: "o WhatsApp pode ter entregue o modelo, mas a resposta se perdeu; confira a conversa antes de enviar de novo"}
	case errors.Is(err, conversation.ErrEntryTypeInvalid):
		return copilot.Result{Status: copilot.StatusError, Message: "modelos só podem ser enviados em conversas do WhatsApp oficial"}
	}
	log.Printf("[copilot] send_template failed: %v", err)
	return copilot.Result{Status: copilot.StatusError, Message: "falha ao enviar o modelo"}
}

func (t *sendTemplateTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	_, err := validateArgs[sendTemplateArgs](t.deps.Entries, cc, args)
	return err
}

func (t *sendTemplateTool) Preview(_ context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	var a sendTemplateArgs
	bindArgs(args, &a)
	template, err := t.template(cc, a.TemplateID)
	if err != nil || template == nil {
		return nil
	}
	return &copilot.Preview{Kind: copilot.PreviewWhatsAppTemplate, Data: templatePreviewOf(template, a.Variables)}
}
