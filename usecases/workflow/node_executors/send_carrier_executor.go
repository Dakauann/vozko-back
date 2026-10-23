package node_executors

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/workflow"
)

const (
	CarrierReasonUnavailable      = "carrier_unavailable"
	CarrierReasonConfigUnreadable = "config_unreadable"
	CarrierReasonLeadNotFound     = "lead_not_found"
	CarrierReasonPhoneMissing     = "lead_phone_missing"
	CarrierReasonRefused          = "carrier_refused"
	CarrierReasonRejected         = "carrier_rejected"
	CarrierReasonQuietHours       = "carrier_quiet_hours"
)

var (
	ErrCarrierUnavailable = errors.New("RCS and SMS sending is not available in this context")
	errCarrierNoLead      = errors.New("the run has no lead to message")
	errCarrierNoPhone     = errors.New("the lead has no phone number")
)

type CarrierDelivery struct {
	WorkspaceID string
	Channel     carriermessaging.Channel
	SenderID    string
	Phone       string
	ContactName string
	Lead        *lead_domain.Lead
	Marketing   bool
	RunID       string
	Content     rcs.Content
	Text        string
	SentBy      conversation.SentBy
}

type CarrierSender interface {
	DeliverFromWorkflow(ctx context.Context, delivery CarrierDelivery) (*carriermessaging.Message, error)
}

type CarrierSenderRelay struct {
	mu     sync.RWMutex
	target CarrierSender
}

func NewCarrierSenderRelay() *CarrierSenderRelay {
	return &CarrierSenderRelay{}
}

func (r *CarrierSenderRelay) Bind(target CarrierSender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
}

func (r *CarrierSenderRelay) DeliverFromWorkflow(ctx context.Context, delivery CarrierDelivery) (*carriermessaging.Message, error) {
	r.mu.RLock()
	target := r.target
	r.mu.RUnlock()
	if target == nil {
		return nil, ErrCarrierUnavailable
	}
	return target.DeliverFromWorkflow(ctx, delivery)
}

type CarrierLeads interface {
	FindByID(workspaceID, id string) (*lead_domain.Lead, error)
}

type CarrierNodeDeps struct {
	Sender     CarrierSender
	Leads      CarrierLeads
	EntryLeads lead_domain.EntryLeads
}

type sendCarrierExecutor struct {
	deps CarrierNodeDeps
}

func NewSendCarrierExecutor(deps CarrierNodeDeps) workflow.NodeExecutor {
	return &sendCarrierExecutor{deps: deps}
}

func (e *sendCarrierExecutor) Definition() workflow.NodeDefinition {
	onRCS := &workflow.FieldRule{Field: workflow.CarrierChannelKey, Values: []string{string(carriermessaging.ChannelRCS)}}
	onSMS := &workflow.FieldRule{Field: workflow.CarrierChannelKey, Values: []string{string(carriermessaging.ChannelSMS)}}
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionSendCarrier,
		TestBehavior: workflow.NodeTestSendsMessage,
		Category:     workflow.NodeCategoryMessaging,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Enviar RCS ou SMS",
		Description:  "Envia uma mensagem RCS ou um SMS ao telefone do lead, mesmo sem conversa aberta.",
		Icon:         "ChatText",
		Guidance: workflow.NodeGuidance{
			When: "Para falar com o lead por RCS (texto, mídia, cartões e botões) ou SMS, por exemplo depois dos gatilhos Aniversário ou Webhook iniciando em Lead.",
			Behavior: "Envia ao telefone do cadastro do lead pelo remetente RCS ou pelo remetente de SMS escolhido, com as mesmas regras de envio, cobrança, opt-out, limite de frequência e horário dos envios de RCS e SMS. " +
				"Os textos aceitam variáveis {{...}}. Saídas: 'sent' quando o envio foi aceito e 'failed' quando foi recusado; o motivo fica em reason e error. Fora do horário de envio, uma mensagem de marketing sai por failed com reason carrier_quiet_hours e next_open; ligue um nó Aguardar e volte para este nó para tentar de novo.",
			Examples: []string{
				"config: {\"channel\":\"sms\",\"route_id\":\"<id>\",\"text\":\"Oi {{lead_name}}, seu cupom: {{event.cupom}}\",\"marketing\":true}",
			},
		},
		DefaultConfig: map[string]interface{}{
			workflow.CarrierChannelKey:   defaultCarrierChannel(),
			workflow.CarrierSenderKey:    "",
			workflow.CarrierRouteKey:     "",
			workflow.CarrierContentKey:   map[string]interface{}{"text": ""},
			workflow.CarrierTextKey:      "",
			workflow.CarrierMarketingKey: true,
		},
		Outputs: []workflow.HandleDefinition{
			{ID: workflow.CarrierHandleSent, Label: "Enviado"},
			{ID: workflow.CarrierHandleFailed, Label: "Falha", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "sent", Description: "true quando o envio foi aceito", Label: "Enviado", Type: workflow.VariableTypeBoolean},
			{Key: "message_id", Description: "ID da mensagem RCS ou SMS", Label: "ID da mensagem", Type: workflow.VariableTypeText},
			{Key: "channel", Description: "rcs ou sms", Label: "Canal", Type: workflow.VariableTypeText},
			{Key: "status", Description: "Situação da mensagem no envio", Label: "Situação do envio", Type: workflow.VariableTypeText},
			{Key: "reason", Description: "Código do motivo quando o envio falha", Label: "Motivo", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição da recusa quando o envio falha", Label: "Erro", Type: workflow.VariableTypeText},
			{Key: "next_open", Description: "Quando reason é carrier_quiet_hours: próximo horário de envio (RFC 3339)", Label: "Próximo horário de envio", Type: workflow.VariableTypeDate},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: workflow.CarrierChannelKey, Label: "Canal", Type: "select", Required: true, Options: carrierChannelOptions()},
			{Key: workflow.CarrierSenderKey, Label: "Remetente RCS", Type: "select", OptionsSource: "rcs_senders", VisibleWhen: onRCS, RequiredWhen: onRCS},
			{Key: workflow.CarrierContentKey, Label: "Mensagem RCS", Type: "rcs_content", VisibleWhen: onRCS},
			{Key: workflow.CarrierRouteKey, Label: "Remetente de SMS", Type: "select", OptionsSource: "sms_routes", VisibleWhen: onSMS, RequiredWhen: onSMS},
			{Key: workflow.CarrierTextKey, Label: "Texto do SMS", Type: "textarea", VisibleWhen: onSMS, RequiredWhen: onSMS},
			{Key: workflow.CarrierMarketingKey, Label: "Mensagem de marketing", Type: "boolean",
				Description: "Marketing respeita opt-out, limite de frequência e horário de envio. Desligue só para mensagens de serviço, como confirmação de pedido."},
		},
	}
}

func (e *sendCarrierExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if ctx == nil || ctx.Run == nil || ctx.Node == nil {
		return nil, workflow.ErrNodeConfigMissing
	}
	send, err := workflow.ReadCarrierSend(ctx.Node.Config)
	if err != nil {
		return carrierFailed(ctx, "", CarrierReasonConfigUnreadable, err), nil
	}
	if e.deps.Sender == nil || e.deps.Leads == nil {
		return carrierFailed(ctx, send.Channel, CarrierReasonUnavailable, ErrCarrierUnavailable), nil
	}
	contact, err := e.leadOf(ctx.Run)
	if err != nil {
		return carrierFailed(ctx, send.Channel, CarrierReasonLeadNotFound, err), nil
	}
	if strings.TrimSpace(contact.Number) == "" {
		return carrierFailed(ctx, send.Channel, CarrierReasonPhoneMissing, errCarrierNoPhone), nil
	}

	rendered := send.Render(customerTextIn(ctx))
	delivery := CarrierDelivery{
		WorkspaceID: ctx.Run.WorkspaceID,
		Channel:     send.Channel,
		SenderID:    send.SenderID,
		Phone:       contact.Number,
		ContactName: contact.RealName(),
		Lead:        contact,
		Marketing:   send.Marketing,
		RunID:       ctx.Run.ID,
		SentBy:      conversation.SentByWorkflow(workflowIDOf(ctx)),
	}
	if send.Channel == carriermessaging.ChannelSMS {
		delivery.Text = rendered.Text
	} else {
		delivery.Content = rendered.Content
	}

	callCtx, release := ctx.CallContext()
	defer release()
	message, err := e.deps.Sender.DeliverFromWorkflow(callCtx, delivery)
	if err != nil {
		log.Printf("[workflow][node:%s][run:%s] send_carrier refused: %v", ctx.Node.ID, ctx.Run.ID, err)
		failed := carrierFailed(ctx, send.Channel, refusalReason(err), err)
		var quiet *carriermessaging.QuietHoursError
		if errors.As(err, &quiet) {
			failed.Output["next_open"] = quiet.NextOpen.UTC().Format(time.RFC3339)
		}
		return failed, nil
	}
	if message == nil || message.Status.Terminal() {
		return carrierFailed(ctx, send.Channel, CarrierReasonRejected, rejectionOf(message)), nil
	}
	output := map[string]interface{}{
		"sent":       true,
		"message_id": message.ID,
		"channel":    string(send.Channel),
		"status":     string(message.Status),
	}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), workflow.CarrierHandleSent), nil
}

func (e *sendCarrierExecutor) leadOf(run *workflow.WorkflowRun) (*lead_domain.Lead, error) {
	leadID := ""
	if run.OnLead() {
		leadID = run.EntryID
	} else if e.deps.EntryLeads != nil {
		found, err := e.deps.EntryLeads.LeadOfEntry(context.Background(), run.WorkspaceID, runEntry(run))
		if err != nil {
			return nil, err
		}
		leadID = found
	}
	if strings.TrimSpace(leadID) == "" {
		return nil, errCarrierNoLead
	}
	contact, err := e.deps.Leads.FindByID(run.WorkspaceID, leadID)
	if err != nil {
		return nil, err
	}
	if contact == nil {
		return nil, errCarrierNoLead
	}
	return contact, nil
}

func workflowIDOf(ctx *workflow.NodeContext) string {
	if ctx.Workflow != nil && ctx.Workflow.ID != "" {
		return ctx.Workflow.ID
	}
	return ctx.Run.WorkflowID
}

func rejectionOf(message *carriermessaging.Message) error {
	if message == nil {
		return errors.New("the carrier returned no message")
	}
	return fmt.Errorf("the message ended %s with code %q", message.Status, message.ErrorCode)
}

func carrierFailed(ctx *workflow.NodeContext, channel carriermessaging.Channel, reason string, cause error) *workflow.NodeResult {
	output := map[string]interface{}{
		"sent":    false,
		"channel": string(channel),
		"reason":  reason,
		"error":   cause.Error(),
	}
	return routeOutput(&workflow.NodeResult{Output: output}, outgoingEdges(ctx), workflow.CarrierHandleFailed)
}

func refusalReason(err error) string {
	switch {
	case errors.Is(err, ErrCarrierUnavailable):
		return CarrierReasonUnavailable
	case errors.Is(err, carriermessaging.ErrQuietHours):
		return CarrierReasonQuietHours
	}
	return CarrierReasonRefused
}

var carrierChannelLabels = map[carriermessaging.Channel]string{
	carriermessaging.ChannelRCS: "RCS",
	carriermessaging.ChannelSMS: "SMS",
}

func carrierChannelOptions() []workflow.ConfigFieldOption {
	var out []workflow.ConfigFieldOption
	for _, c := range carriermessaging.LaunchedChannels() {
		out = append(out, workflow.ConfigFieldOption{Value: string(c), Label: carrierChannelLabels[c]})
	}
	return out
}

func defaultCarrierChannel() string {
	if launched := carriermessaging.LaunchedChannels(); len(launched) > 0 {
		return string(launched[0])
	}
	return ""
}
