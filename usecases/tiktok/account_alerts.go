package tiktok

import (
	"context"
	"time"

	"vozko/brand"
	"vozko/domain/config"
	"vozko/domain/notification"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workspace"
)

const AccountsPath = "/dashboard/tiktok-accounts"

type accountAlerts struct {
	publisher notification.EpisodePublisher
}

func NewAccountAlerts(publisher notification.EpisodePublisher) AccountAlerts {
	if publisher == nil {
		panic("tiktok: account alerts require the notification publisher")
	}
	return &accountAlerts{publisher: publisher}
}

func reconnectEpisodeKey(accountID string) string     { return "tiktok_reconnect:" + accountID }
func reconnectSoonEpisodeKey(accountID string) string { return "tiktok_reconnect_soon:" + accountID }
func webhooksEpisodeKey(accountID string) string      { return "tiktok_webhooks:" + accountID }

func (a *accountAlerts) NeedsReconnect(ctx context.Context, account *ttdomain.Account) error {
	if account == nil || (account.Status != ttdomain.StatusNeedsReconnect && account.Status != ttdomain.StatusRevoked) {
		return nil
	}
	inBrand := brand.Active().Portuguese().InBrand
	name := account.Name()
	title := "Reconecte a conta " + name + " do TikTok"
	body := "O TikTok deixou de aceitar o acesso da conta " + name + ". Até a conta ser conectada de novo, " +
		"as mensagens diretas e os comentários param de chegar " + inBrand + " e as respostas não são enviadas."
	if account.Status == ttdomain.StatusRevoked {
		title = "O acesso da conta " + name + " do TikTok foi revogado"
		body = "A autorização da conta " + name + " foi revogada no TikTok e as mensagens e os comentários pararam de chegar " + inBrand + "."
		if account.PurgeAfter != nil {
			body += " Se a conta não for conectada de novo até " + formatDate(*account.PurgeAfter) +
				", as conversas, os contatos e os comentários dela serão apagados."
		}
	}
	return a.publish(ctx, account, notification.Draft{
		Severity:    notification.SeverityCritical,
		Title:       title,
		Body:        body,
		ActionLabel: "Reconectar conta",
		Email:       true,
		EpisodeKey:  reconnectEpisodeKey(account.ID),
	})
}

func (a *accountAlerts) ReconnectSoon(ctx context.Context, account *ttdomain.Account) error {
	if account == nil || account.RefreshExpiresAt == nil {
		return nil
	}
	if err := a.resolve(ctx, account, reconnectEpisodeKey(account.ID), webhooksEpisodeKey(account.ID)); err != nil {
		return err
	}
	name := account.Name()
	return a.publish(ctx, account, notification.Draft{
		Severity: notification.SeverityWarning,
		Title:    "A conexão da conta " + name + " do TikTok vence em breve",
		Body: "O TikTok exige uma nova autorização da conta " + name + " até " + formatDate(*account.RefreshExpiresAt) +
			". Reconecte a conta antes dessa data para as mensagens diretas e os comentários continuarem chegando " +
			brand.Active().Portuguese().InBrand + ".",
		ActionLabel: "Reconectar conta",
		Email:       true,
		EpisodeKey:  reconnectSoonEpisodeKey(account.ID),
	})
}

func (a *accountAlerts) WebhooksUnhealthy(ctx context.Context, account *ttdomain.Account) error {
	if account == nil {
		return nil
	}
	if err := a.resolve(ctx, account, reconnectEpisodeKey(account.ID)); err != nil {
		return err
	}
	name := account.Name()
	return a.publish(ctx, account, notification.Draft{
		Severity: notification.SeverityWarning,
		Title:    "Mensagens da conta " + name + " do TikTok podem atrasar",
		Body: "O TikTok não confirmou o envio de avisos de mensagens e comentários da conta " + name + ". " +
			"Estamos tentando restabelecer automaticamente; enquanto isso, mensagens novas podem demorar a aparecer " +
			brand.Active().Portuguese().InBrand + ".",
		ActionLabel: "Ver conta",
		EpisodeKey:  webhooksEpisodeKey(account.ID),
	})
}

func (a *accountAlerts) Healthy(ctx context.Context, account *ttdomain.Account) error {
	if account == nil {
		return nil
	}
	return a.resolve(ctx, account, reconnectEpisodeKey(account.ID), reconnectSoonEpisodeKey(account.ID), webhooksEpisodeKey(account.ID))
}

func (a *accountAlerts) publish(ctx context.Context, account *ttdomain.Account, d notification.Draft) error {
	d.WorkspaceID = account.WorkspaceID
	d.Category = notification.CategoryChannels
	d.Title = notification.ClipTitle(d.Title)
	d.ActionURL = AccountsPath + "/" + account.ID
	d.Audience = notification.Holding(workspace.ResourceTikTokAccounts, workspace.ActionCreate)
	d.Source = notification.SourceTikTok
	_, err := a.publisher.Publish(ctx, d)
	return err
}

func (a *accountAlerts) resolve(ctx context.Context, account *ttdomain.Account, keys ...string) error {
	return a.publisher.Resolve(ctx, account.WorkspaceID, keys...)
}

func formatDate(t time.Time) string {
	return t.In(config.BrazilianTimezone).Format("02/01/2006")
}
