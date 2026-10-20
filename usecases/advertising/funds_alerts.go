package advertising

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"vozko/brand"
	ads "vozko/domain/advertising"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

const fundsAlertTemplate = "meta_ads_funds_alert.html"

type fundsCopy struct {
	headline  string
	subtitle  string
	situation string
	message   func(account, room string) string
	tone      string
	action    string
	inVozko   bool
}

var fundsCopies = map[ads.FundsReason]fundsCopy{
	ads.ReasonFundsLow: {
		headline:  "Fundos da conta de anúncios acabando",
		subtitle:  "Adicione fundos na Meta para os anúncios não pararem",
		situation: "Fundos acabando",
		message: func(account, room string) string {
			return "Os fundos da conta de anúncios " + account + " na Meta pagam só mais " + room + " em anúncios. Quando acabarem, a Meta pausa todos os anúncios desta conta até você adicionar fundos."
		},
		tone:   "warning",
		action: "Adicionar fundos na Meta",
	},
	ads.ReasonFundsOut: {
		headline:  "Os anúncios pararam: os fundos acabaram",
		subtitle:  "Adicione fundos na Meta para voltar a veicular",
		situation: "Sem fundos",
		message: func(account, _ string) string {
			return "Os fundos da conta de anúncios " + account + " na Meta acabaram e os anúncios pararam de veicular. A Meta volta a veicular sozinha depois que você adicionar fundos."
		},
		tone:   "danger",
		action: "Adicionar fundos na Meta",
	},
	ads.ReasonSpendLimitLow: {
		headline:  "Limite de gastos da conta quase atingido",
		subtitle:  "Aumente ou remova o limite para os anúncios não pararem",
		situation: "Limite quase atingido",
		message: func(account, room string) string {
			return "A conta de anúncios " + account + " pode gastar só mais " + room + " antes de atingir o limite de gastos da conta. Ao atingir, a Meta para todos os anúncios desta conta."
		},
		tone:    "warning",
		action:  "Ajustar o limite",
		inVozko: true,
	},
	ads.ReasonSpendLimitReached: {
		headline:  "Limite de gastos da conta atingido",
		subtitle:  "Aumente ou remova o limite para voltar a veicular",
		situation: "Limite atingido",
		message: func(account, _ string) string {
			return "A conta de anúncios " + account + " atingiu o limite de gastos da conta e a Meta parou todos os anúncios dela. Aumente ou remova o limite para voltar a veicular."
		},
		tone:    "danger",
		action:  "Ajustar o limite",
		inVozko: true,
	},
	ads.ReasonPaymentFailed: {
		headline:  "Pagamento dos anúncios recusado",
		subtitle:  "Pague o valor em aberto na Meta para voltar a veicular",
		situation: "Pagamento em aberto",
		message: func(account, _ string) string {
			return "A Meta não conseguiu cobrar a conta de anúncios " + account + " e os anúncios estão parados até o pagamento. Pague o valor em aberto na Meta para voltar a veicular."
		},
		tone:   "danger",
		action: "Pagar na Meta",
	},
	ads.ReasonGracePeriod: {
		headline:  "Pagamento dos anúncios pendente",
		subtitle:  "Pague na Meta antes que os anúncios parem",
		situation: "Em período de carência",
		message: func(account, _ string) string {
			return "A Meta não conseguiu cobrar a conta de anúncios " + account + ". Os anúncios seguem no ar por um período de carência curto e param se o pagamento não for feito."
		},
		tone:   "warning",
		action: "Pagar na Meta",
	},
}

type FundsAlerts struct {
	publisher    notification.EpisodePublisher
	dashboardURL string
}

func NewFundsAlerts(publisher notification.EpisodePublisher, dashboardURL string) *FundsAlerts {
	return &FundsAlerts{publisher: publisher, dashboardURL: strings.TrimRight(dashboardURL, "/")}
}

func (a *FundsAlerts) Alert(ctx context.Context, account *ads.AdAccount, funds ads.Funds) error {
	text, ok := fundsCopies[funds.Reason]
	if !funds.Level.NeedsAttention() || !ok {
		return nil
	}
	if err := a.publisher.Resolve(ctx, account.WorkspaceID, fundsEpisodeKeys(account.ID, funds.Reason)...); err != nil {
		return err
	}
	room := ""
	if funds.RoomMinor != nil {
		room = formatMinor(account.Currency, *funds.RoomMinor)
	}
	managerPath := "/dashboard/advertising?account=" + account.ID
	action := account.PortalURL(ads.PortalBilling)
	if text.inVozko {
		action = "/dashboard/advertising/overview?account=" + account.ID
	}
	severity := notification.SeverityWarning
	if text.tone == "danger" {
		severity = notification.SeverityCritical
	}
	message := text.message(account.Name, room)
	_, err := a.publisher.Publish(ctx, notification.Draft{
		WorkspaceID: account.WorkspaceID,
		Category:    notification.CategoryAdvertising,
		Severity:    severity,
		Title:       notification.ClipTitle(text.headline),
		Body:        message,
		ActionURL:   action,
		ActionLabel: text.action,
		Audience:    notification.Holding(workspace.ResourceAds, workspace.ActionUpdate),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: fundsAlertTemplate,
			Subject:  text.headline + " - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Headline":    text.headline,
				"Subtitle":    text.subtitle,
				"Message":     message,
				"Situation":   text.situation,
				"Tone":        text.tone,
				"AccountName": account.Name,
				"ActionURL":   notification.AbsoluteLink(a.dashboardURL, action),
				"ActionLabel": text.action,
				"ManagerURL":  notification.AbsoluteLink(a.dashboardURL, managerPath),
			},
		},
		EpisodeKey: fundsEpisodeKey(account.ID, funds.Reason),
		Source:     notification.SourceAdsFunds,
	})
	return err
}

func (a *FundsAlerts) Recovered(ctx context.Context, account *ads.AdAccount) error {
	return a.publisher.Resolve(ctx, account.WorkspaceID, fundsEpisodeKeys(account.ID, "")...)
}

func fundsEpisodeKey(accountID string, reason ads.FundsReason) string {
	return "meta_ads_funds:" + accountID + ":" + string(reason)
}

func fundsEpisodeKeys(accountID string, except ads.FundsReason) []string {
	var keys []string
	for _, reason := range slices.Sorted(maps.Keys(fundsCopies)) {
		if reason != except {
			keys = append(keys, fundsEpisodeKey(accountID, reason))
		}
	}
	return keys
}

func formatMinor(currency string, minor int64) string {
	code := strings.ToUpper(currency)
	prefix := map[string]string{"BRL": "R$", "USD": "US$"}[code]
	if prefix == "" {
		prefix = code
	}
	microsPerMinor, err := ads.MinorToMicros(code, 1)
	if err != nil || microsPerMinor == 1_000_000 {
		return prefix + " " + strconv.FormatInt(minor, 10)
	}
	return prefix + " " + strings.Replace(fmt.Sprintf("%.2f", float64(minor)/100), ".", ",", 1)
}
