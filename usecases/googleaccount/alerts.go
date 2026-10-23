package googleaccount

import (
	"context"
	"errors"

	"vozko/brand"
	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

type Alerts interface {
	NeedsReconnect(ctx context.Context, account *googleaccount.Account, products []googleaccount.Product) error
	Reconnected(ctx context.Context, account *googleaccount.Account, product googleaccount.Product) error
}

type productCopy struct {
	name     string
	stops    string
	resource workspace.Resource
	path     string
}

var productCopies = map[googleaccount.Product]productCopy{
	googleaccount.ProductCalendar: {
		name:     "Google Agenda",
		stops:    "os agendamentos feitos por agentes, fluxos e pela equipe deixam de ir para o Google Agenda",
		resource: workspace.ResourceCalendar,
		path:     IntegrationsPath,
	},
	googleaccount.ProductSheets: {
		name:     "Google Planilhas",
		stops:    "as linhas que agentes e fluxos registram ficam guardadas e esperando, sem chegar às planilhas",
		resource: workspace.ResourceGoogleSheets,
		path:     googlesheet.DashboardPath,
	},
}

type alerts struct {
	publisher notification.EpisodePublisher
}

func NewAlerts(publisher notification.EpisodePublisher) Alerts {
	if publisher == nil {
		panic("googleaccount: alerts require the notification publisher")
	}
	return &alerts{publisher: publisher}
}

func reconnectEpisodeKey(product googleaccount.Product, accountID string) string {
	return "google_reconnect:" + string(product) + ":" + accountID
}

func (a *alerts) NeedsReconnect(ctx context.Context, account *googleaccount.Account, products []googleaccount.Product) error {
	if account == nil || account.Status != googleaccount.StatusNeedsReconnect {
		return nil
	}
	var errs []error
	for _, product := range products {
		text, ok := productCopies[product]
		if !ok {
			continue
		}
		_, err := a.publisher.Publish(ctx, notification.Draft{
			WorkspaceID: account.WorkspaceID,
			Category:    notification.CategoryChannels,
			Severity:    notification.SeverityCritical,
			Title:       notification.ClipTitle("Reconecte o " + text.name),
			Body: "O Google deixou de aceitar o acesso da conta " + account.Email + " " + brand.Active().Portuguese().InBrand + ". " +
				"Até alguém conectar a conta de novo, " + text.stops + ". Isso costuma acontecer quando a senha muda ou o acesso é removido na conta Google.",
			ActionURL:   text.path,
			ActionLabel: "Reconectar",
			Audience:    notification.Holding(text.resource, workspace.ActionCreate),
			Email:       true,
			EpisodeKey:  reconnectEpisodeKey(product, account.ID),
			Source:      notification.SourceGoogleAccount,
		})
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *alerts) Reconnected(ctx context.Context, account *googleaccount.Account, product googleaccount.Product) error {
	if account == nil {
		return nil
	}
	return a.publisher.Resolve(ctx, account.WorkspaceID, reconnectEpisodeKey(product, account.ID))
}
