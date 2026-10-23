package mercadolivre

import (
	"context"
	"time"

	"vozko/brand"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

const AccountsPath = "/dashboard/mercadolivre-accounts"

type accountAlerts struct {
	publisher notification.EpisodePublisher
}

func NewAccountAlerts(publisher notification.EpisodePublisher) AccountAlerts {
	if publisher == nil {
		panic("mercadolivre: account alerts require the notification publisher")
	}
	return &accountAlerts{publisher: publisher}
}

func reconnectEpisodeKey(accountID string) string { return "mercadolivre_reconnect:" + accountID }

func (a *accountAlerts) NeedsReconnect(ctx context.Context, account *mldomain.Account) error {
	if account == nil || (account.Status != mldomain.StatusNeedsReconnect && account.Status != mldomain.StatusRevoked) {
		return nil
	}
	inBrand := brand.Active().Portuguese().InBrand
	name := account.Label()
	title := "Reconecte a conta " + name + " do Mercado Livre"
	body := "O Mercado Livre deixou de aceitar o acesso da conta " + name + ". Até o administrador da conta conectá-la de novo, " +
		"as mensagens pós-venda param de chegar " + inBrand + " e as respostas não são enviadas. " +
		"Isso costuma acontecer depois de uma troca de senha no Mercado Livre."
	if account.Status == mldomain.StatusRevoked {
		title = "O acesso da conta " + name + " do Mercado Livre foi revogado"
		body = "A autorização da conta " + name + " foi revogada no Mercado Livre e as mensagens pararam de chegar " + inBrand + "."
		if account.PurgeAfter != nil {
			body += " Se a conta não for conectada de novo até " + account.PurgeAfter.In(brasilia()).Format("02/01/2006") +
				", as conversas e os contatos dela serão apagados, como exigem os termos do Mercado Livre."
		}
	}
	_, err := a.publisher.Publish(ctx, notification.Draft{
		WorkspaceID: account.WorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notification.SeverityCritical,
		Title:       notification.ClipTitle(title),
		Body:        body,
		ActionURL:   AccountsPath + "/" + account.ID,
		ActionLabel: "Reconectar conta",
		Audience:    notification.Holding(workspace.ResourceMercadoLivreAccounts, workspace.ActionCreate),
		Email:       true,
		EpisodeKey:  reconnectEpisodeKey(account.ID),
		Source:      notification.SourceMercadoLivre,
	})
	return err
}

func (a *accountAlerts) Reconnected(ctx context.Context, account *mldomain.Account) error {
	if account == nil {
		return nil
	}
	return a.publisher.Resolve(ctx, account.WorkspaceID, reconnectEpisodeKey(account.ID))
}

func brasilia() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}
