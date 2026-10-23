package carriermessaging_usecase

import (
	"context"
	"log"
	"strconv"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/notification"
	"vozko/domain/rcs"
	"vozko/domain/workspace"
)

const (
	SendersPath     = "/dashboard/rcs-sms/rcs-senders"
	SettingsPath    = "/dashboard/rcs-sms/settings"
	confirmationKey = "carrier_sender_confirm:"
	rejectedKey     = "carrier_sender_rejected:"
	pausedKey       = "carrier_sender_paused:"
	floodKey        = "carrier_flood:"
)

type episodeNotices struct {
	publisher notification.EpisodePublisher
}

func NewNotices(publisher notification.EpisodePublisher) Notices {
	return &episodeNotices{publisher: publisher}
}

func (n *episodeNotices) SenderStateChanged(ctx context.Context, s rcs.Sender) {
	brand := strings.TrimSpace(s.BrandName)
	keys := []string{confirmationKey + s.ID, rejectedKey + s.ID, pausedKey + s.ID}
	switch s.Status {
	case rcs.SenderAwaitingConfirmation:
		n.publish(ctx, s, notification.Draft{
			Severity: notification.SeverityWarning,
			Title:    "Confirme a marca " + brand + " para liberar o RCS",
			Body: "As operadoras enviaram um e-mail de rcs@abrtelecom.com.br para " + s.ContactEmail +
				". Abra o e-mail e clique em CONFIRMO; o remetente costuma ser ativado em até 48 horas úteis depois disso.",
			ActionLabel: "Ver remetente", Email: true, EpisodeKey: keys[0],
		})
	case rcs.SenderRejected:
		n.publish(ctx, s, notification.Draft{
			Severity:    notification.SeverityCritical,
			Title:       "O remetente RCS " + brand + " foi recusado",
			Body:        "Motivo informado: " + reasonOrUnknown(s.StatusReason) + ". Corrija os dados da marca e envie de novo para aprovação.",
			ActionLabel: "Corrigir remetente", Email: true, EpisodeKey: keys[1],
		})
	case rcs.SenderPaused:
		n.publish(ctx, s, notification.Draft{
			Severity:    notification.SeverityCritical,
			Title:       "O remetente RCS " + brand + " foi pausado",
			Body:        "Os envios por este remetente estão parados. Motivo: " + pauseReason(s) + ".",
			ActionLabel: "Ver remetente", Email: true, EpisodeKey: keys[2],
		})
	case rcs.SenderLaunched:
		n.resolve(ctx, s.WorkspaceID, keys...)
		n.publish(ctx, s, notification.Draft{
			Severity:    notification.SeverityInfo,
			Title:       "O remetente RCS " + brand + " está ativo",
			Body:        "As operadoras aprovaram a marca " + brand + ". Já é possível enviar campanhas e responder conversas por RCS.",
			ActionLabel: "Criar campanha",
		})
	case rcs.SenderSubmitted:
		n.resolve(ctx, s.WorkspaceID, keys[1])
		n.publish(ctx, s, notification.Draft{
			Severity:    notification.SeverityInfo,
			Title:       "O remetente RCS " + brand + " foi enviado para aprovação",
			Body:        "Em seguida as operadoras mandam um e-mail para " + s.ContactEmail + " pedindo a confirmação da marca.",
			ActionLabel: "Acompanhar",
		})
	}
}

func (n *episodeNotices) ContactFlooding(ctx context.Context, workspaceID string, channel carriermessaging.Channel, contact carriermessaging.Contact, inboundInWindow int) {
	channelName := "RCS"
	if channel == carriermessaging.ChannelSMS {
		channelName = "SMS"
	}
	d := notification.Draft{
		WorkspaceID: workspaceID, Category: notification.CategoryChannels, Severity: notification.SeverityWarning,
		Title: "Um contato está enviando mensagens demais por " + channelName,
		Body: "O número " + contact.Phone + " enviou " + strconv.Itoa(inboundInWindow) + " mensagens na última hora. " +
			"A automação parou de responder a este contato para não gerar mais custo; cada mensagem recebida continua sendo cobrada pela operadora.",
		ActionURL: SettingsPath, ActionLabel: "Ver regras de envio", Email: true, EpisodeKey: floodKey + contact.ID,
		Audience: notification.Holding(workspace.ResourceCarrierMessaging, workspace.ActionConfigure),
		Source:   notification.SourceCarrierFlood,
	}
	d.Title = notification.ClipTitle(d.Title)
	if _, err := n.publisher.Publish(ctx, d); err != nil {
		log.Printf("[carrier] flood notice for %s: %v", contact.ID, err)
	}
	log.Printf("CRITICAL: [carrier] contact %s of workspace %s sent %d %s messages in an hour", contact.ID, workspaceID, inboundInWindow, channelName)
}

func (n *episodeNotices) publish(ctx context.Context, s rcs.Sender, d notification.Draft) {
	d.WorkspaceID = s.WorkspaceID
	d.Category = notification.CategoryChannels
	d.Title = notification.ClipTitle(d.Title)
	d.ActionURL = SendersPath + "/" + s.ID
	d.Audience = notification.Holding(workspace.ResourceCarrierMessaging, workspace.ActionConfigure)
	d.Source = notification.SourceCarrierSender
	if _, err := n.publisher.Publish(ctx, d); err != nil {
		log.Printf("[carrier] sender notice for %s: %v", s.ID, err)
	}
}

func (n *episodeNotices) resolve(ctx context.Context, workspaceID string, keys ...string) {
	if err := n.publisher.Resolve(ctx, workspaceID, keys...); err != nil {
		log.Printf("[carrier] resolve %v: %v", keys, err)
	}
}

func reasonOrUnknown(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "não informado pela operadora"
	}
	return strings.TrimSpace(reason)
}

var pauseReasons = map[rcs.PauseReason]string{
	rcs.PauseManual:        "pausado no workspace",
	rcs.PauseComplaints:    "reclamações de destinatários",
	rcs.PauseOptOutRate:    "muitos pedidos de descadastro em uma campanha",
	rcs.PauseUnpaidBalance: "saldo em aberto",
	rcs.PauseCarrier:       "pausado pela operadora",
}

func pauseReason(s rcs.Sender) string {
	if text, ok := pauseReasons[s.PauseReason]; ok {
		if s.StatusReason != "" {
			return text + " (" + s.StatusReason + ")"
		}
		return text
	}
	return reasonOrUnknown(s.StatusReason)
}
