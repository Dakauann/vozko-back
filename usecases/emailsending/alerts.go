package emailsending

import (
	"context"
	"fmt"
	"log"
	"strings"

	es "vozko/domain/emailsending"
	"vozko/domain/notification"
)

const (
	SendersPath        = "/dashboard/email/senders"
	sourceEmailSending = "email_sending"
)

func tenantPausedKey(workspaceID string) string { return "email_tenant_paused:" + workspaceID }

func findingKey(workspaceID, findingType string) string {
	return "email_reputation_finding:" + workspaceID + ":" + strings.ToLower(strings.TrimSpace(findingType))
}

func domainFailedKey(domainID string) string { return "email_domain_failed:" + domainID }

func spamRateKey(domainID string) string { return "email_spam_rate:" + domainID }

func (c *core) publish(ctx context.Context, d notification.Draft) {
	d.Audience = notification.Managers()
	d.Source = sourceEmailSending
	d.Email = true
	d.Title = notification.ClipTitle(d.Title)
	if d.ActionURL == "" {
		d.ActionURL = SendersPath
	}
	if _, err := c.Alerts.Publish(ctx, d); err != nil {
		log.Printf("[emailsending] notification %q for workspace %s failed: %v", d.Title, d.WorkspaceID, err)
	}
}

func (c *core) resolve(ctx context.Context, workspaceID string, keys ...string) {
	if err := c.Alerts.Resolve(ctx, workspaceID, keys...); err != nil {
		log.Printf("[emailsending] resolving %v for workspace %s failed: %v", keys, workspaceID, err)
	}
}

func (c *core) alertTenantPaused(ctx context.Context, acct *es.SenderAccount, cause string) {
	body := "O provedor de e-mail pausou o envio desta conta por causa da reputação de envio. Campanhas e envios automáticos ficam parados até a conta voltar."
	if strings.TrimSpace(cause) != "" {
		body += " Motivo informado: " + strings.TrimSpace(cause) + "."
	}
	c.publish(ctx, notification.Draft{
		WorkspaceID: acct.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityCritical,
		Title:       "O envio de e-mails foi pausado",
		Body:        body,
		ActionLabel: "Ver remetentes",
		EpisodeKey:  tenantPausedKey(acct.WorkspaceID),
	})
}

func (c *core) alertFinding(ctx context.Context, acct *es.SenderAccount, f es.FindingEvent) {
	body := "Foi detectado um problema grave de reputação nos envios de e-mail. Se não for corrigido, o envio pode ser pausado."
	if strings.TrimSpace(f.Description) != "" {
		body += " Detalhe: " + strings.TrimSpace(f.Description)
	}
	c.publish(ctx, notification.Draft{
		WorkspaceID: acct.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityWarning,
		Title:       "Risco de pausa no envio de e-mails",
		Body:        body,
		ActionLabel: "Ver remetentes",
		EpisodeKey:  findingKey(acct.WorkspaceID, f.Type),
	})
}

func (c *core) notifyDomainVerified(ctx context.Context, d *es.SendingDomain) {
	c.resolve(ctx, d.WorkspaceID, domainFailedKey(d.ID))
	if d.Kind != es.DomainOwn {
		return
	}
	c.publish(ctx, notification.Draft{
		WorkspaceID: d.WorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notification.SeveritySuccess,
		Title:       "Domínio verificado: " + d.Domain,
		Body:        "O domínio " + d.Domain + " foi verificado e já pode ser usado nos remetentes. O volume diário começa baixo e sobe aos poucos para proteger a reputação.",
		ActionLabel: "Ver remetentes",
		DedupKey:    "email_domain_verified:" + d.ID + ":" + d.VerifiedAt.UTC().Format("20060102T150405"),
	})
}

func (c *core) alertDomainFailed(ctx context.Context, d *es.SendingDomain) {
	c.publish(ctx, notification.Draft{
		WorkspaceID: d.WorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notification.SeverityWarning,
		Title:       "Não foi possível verificar o domínio " + d.Domain,
		Body:        "Os registros DNS do domínio " + d.Domain + " não foram confirmados. Confira os registros e verifique de novo.",
		ActionLabel: "Ver registros",
		EpisodeKey:  domainFailedKey(d.ID),
	})
}

func (c *core) notifyPromoted(ctx context.Context, acct *es.SenderAccount) {
	c.publish(ctx, notification.Draft{
		WorkspaceID: acct.WorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notification.SeveritySuccess,
		Title:       "Seu envio de e-mails subiu de nível",
		Body:        "Depois do período de avaliação, os e-mails passam a sair por um domínio compartilhado de maior reputação, com limite diário maior. Os endereços de resposta antigos continuam funcionando.",
		ActionLabel: "Ver remetentes",
		DedupKey:    "email_promoted:" + acct.ID,
	})
}

func (c *core) alertSpamRate(ctx context.Context, d *es.SendingDomain, stats es.DomainStats) {
	c.publish(ctx, notification.Draft{
		WorkspaceID: d.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    notification.SeverityWarning,
		Title:       "Taxa de spam alta no domínio " + d.Domain,
		Body:        fmt.Sprintf("O Gmail registrou %s de denúncias de spam nos e-mails de %s em %s. Acima de 0,3%% a entrega cai. Revise a lista e o conteúdo antes do próximo envio.", perMilAsPercent(stats.SpamRatePerMil), d.Domain, stats.Day),
		ActionLabel: "Ver remetentes",
		EpisodeKey:  spamRateKey(d.ID),
	})
}

func perMilAsPercent(perMil int) string {
	return strings.Replace(fmt.Sprintf("%.1f%%", float64(perMil)/10), ".", ",", 1)
}
