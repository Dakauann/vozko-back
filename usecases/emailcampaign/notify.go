package emailcampaign_usecase

import (
	"context"

	"vozko/domain/emailcampaign"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	emailsending_usecase "vozko/usecases/emailsending"
)

const notificationSource = "email_campaign"

var holdCopy = map[emailcampaign.HoldReason]string{
	emailcampaign.HoldAccountPaused:     "O envio de e-mails da sua conta está pausado. A campanha volta a enviar sozinha quando o envio for liberado.",
	emailcampaign.HoldSenderUnavailable: "O remetente desta campanha não está pronto para enviar. Verifique o domínio e o remetente; a campanha retoma sozinha quando estiverem verificados.",
	emailcampaign.HoldBalanceShort:      "O saldo da carteira não cobre o próximo lote de 1.000 e-mails desta campanha. Ela continua sozinha depois da recarga.",
}

func campaignLink(campaignID string) string {
	return "/dashboard/email/campaigns/" + campaignID
}

func balanceHoldEpisode(campaignID string) string {
	return "email_campaign_balance:" + campaignID
}

func (s *Service) notifyHold(ctx context.Context, c emailcampaign.Campaign, reason emailcampaign.HoldReason) {
	body, ok := holdCopy[reason]
	if !ok {
		return
	}
	draft := notification.Draft{
		WorkspaceID: c.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityWarning,
		Title:       notification.ClipTitle("Campanha de e-mail em espera: " + c.Name),
		Body:        body,
		ActionURL:   campaignLink(c.ID),
		ActionLabel: "Ver campanha",
		Audience:    notification.Holding(workspace.ResourceEmailMarketing, workspace.ActionStart),
		DedupKey:    "email_campaign_hold:" + c.ID + ":" + string(reason),
		Source:      notificationSource,
	}
	if reason == emailcampaign.HoldBalanceShort {
		draft.Title = notification.ClipTitle("Campanha de e-mail aguardando saldo: " + c.Name)
		draft.ActionURL, draft.ActionLabel = emailsending_usecase.WalletTopUpPath, "Recarregar carteira"
		draft.DedupKey, draft.EpisodeKey = "", balanceHoldEpisode(c.ID)
	}
	s.publish(ctx, draft)
}

func (s *Service) resolveBalanceHold(ctx context.Context, c emailcampaign.Campaign) {
	if err := s.deps.Notifications.Resolve(ctx, c.WorkspaceID, balanceHoldEpisode(c.ID)); err != nil {
		s.logf("could not close the balance notice of %s: %v", c.ID, err)
	}
}

func (s *Service) notifyScheduleFailed(ctx context.Context, c emailcampaign.Campaign, reason emailcampaign.HoldReason) {
	s.publish(ctx, notification.Draft{
		WorkspaceID: c.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityCritical,
		Title:       notification.ClipTitle("Campanha de e-mail agendada não começou: " + c.Name),
		Body:        "A campanha voltou para rascunho porque a verificação de disparo encontrou um impedimento. Abra a revisão para ver o que falta.",
		ActionURL:   campaignLink(c.ID),
		ActionLabel: "Revisar campanha",
		Audience:    notification.Holding(workspace.ResourceEmailMarketing, workspace.ActionStart),
		DedupKey:    "email_campaign_schedule_failed:" + c.ID + ":" + string(reason),
		Source:      notificationSource,
	})
}

func (s *Service) publish(ctx context.Context, d notification.Draft) {
	key := d.DedupKey
	if key == "" {
		key = d.EpisodeKey
	}
	if _, err := s.deps.Notifications.Publish(ctx, d); err != nil {
		s.logf("could not publish a notification for %s: %v", key, err)
	}
}
