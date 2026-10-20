package smartcampaign_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

var brasilia = loadBrasilia()

func loadBrasilia() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}

const holdAdvice = "Revise o motivo, corrija o modelo, o número ou a lista e retome a campanha. Ao retomar, a próxima onda começa no tamanho mínimo."

type HoldNotice struct {
	publisher    notification.EpisodePublisher
	dashboardURL string
}

func NewHoldNotice(publisher notification.EpisodePublisher, dashboardURL string) *HoldNotice {
	return &HoldNotice{publisher: publisher, dashboardURL: strings.TrimRight(dashboardURL, "/")}
}

var _ HoldNotifier = (*HoldNotice)(nil)

func (h *HoldNotice) Held(c *wc.Campaign, wave int, d sc.Decision, at time.Time) {
	primary, _ := d.Primary()
	when := primary.At
	if when.IsZero() {
		when = at
	}
	headline, severity := "Campanha pausada automaticamente", notification.SeverityWarning
	if d.Action == sc.Stop {
		headline, severity = "Campanha encerrada automaticamente", notification.SeverityCritical
	}
	title := notification.ClipTitle(headline + ": " + c.Name)
	reason := Reason(primary)
	path := "/dashboard/whatsapp-campaigns/" + c.ID
	_, err := h.publisher.Publish(context.Background(), notification.Draft{
		WorkspaceID: c.WorkspaceID,
		Category:    notification.CategoryCampaigns,
		Severity:    severity,
		Title:       title,
		Body:        "Motivo: " + reason + ". " + holdAdvice,
		ActionURL:   path,
		ActionLabel: "Ver campanha",
		Audience:    notification.Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "whatsapp_campaign_hold.html",
			Subject:  title,
			Placeholders: map[string]interface{}{
				"Headline":     headline,
				"CampaignName": c.Name,
				"Reason":       reason,
				"HeldAt":       when.In(brasilia).Format("02/01 15:04"),
				"Wave":         waveLabel(wave),
				"Advice":       holdAdvice,
				"DashboardURL": h.dashboardURL + path,
			},
		},
		EpisodeKey: holdEpisodeKey(c.ID, d.Action),
		Source:     notification.SourceSmartCampaign,
	})
	if err != nil {
		log.Printf("[smart-campaign] could not notify the hold of %s: %v", c.ID, err)
	}
}

func (h *HoldNotice) Resumed(c *wc.Campaign) {
	if err := h.publisher.Resolve(context.Background(), c.WorkspaceID, holdEpisodeKey(c.ID, sc.Pause), holdEpisodeKey(c.ID, sc.Stop)); err != nil {
		log.Printf("[smart-campaign] could not close the hold alerts of %s: %v", c.ID, err)
	}
}

func holdEpisodeKey(campaignID string, action sc.Action) string {
	return "smart_campaign_hold:" + campaignID + ":" + string(action)
}

func waveLabel(wave int) string {
	if wave == 0 {
		return "antes da primeira onda"
	}
	return fmt.Sprintf("%d", wave)
}

func Reason(f sc.Finding) string {
	switch f.Rule {
	case sc.RuleSpamLimit:
		return "a Meta limitou o número por spam (131048)"
	case sc.RuleAccountLocked:
		return "a conta do WhatsApp foi bloqueada (131031)"
	case sc.RulePaymentProblem:
		return "há um problema de pagamento na conta da Meta (131042)"
	case sc.RuleAccessRemoved:
		return "o acesso " + brand.Active().Portuguese().OfBrand + " à conta foi removido (erro 10)"
	case sc.RuleTemplateDisabled:
		return "o modelo foi desativado ou rejeitado pela Meta"
	case sc.RuleTemplatePaused:
		return "o modelo foi pausado pela Meta"
	case sc.RuleTemplateInReview:
		return "o modelo voltou para análise da Meta"
	case sc.RuleTemplateRed:
		return "o modelo ficou VERMELHO"
	case sc.RuleTemplateYellow:
		return "o modelo ficou AMARELO"
	case sc.RuleCategoryMarketing:
		return "o modelo foi reclassificado como marketing"
	case sc.RulePhoneBanned:
		return "o número foi banido"
	case sc.RulePhoneUnavailable:
		return "o número não pode enviar agora (restrito ou desconectado)"
	case sc.RuleTemplateFlagged:
		return "a Meta sinalizou o modelo por avaliações negativas e ele corre risco de ser desativado"
	case sc.RuleFailureRateStop, sc.RuleFailureRateSlow:
		return fmt.Sprintf("%s das mensagens da onda falharam (limite %s)", percent(f.Observed), percent(f.Limit))
	case sc.RuleEngagementLimit:
		return fmt.Sprintf("a Meta segurou %s das mensagens por engajamento (131049, limite %s)", percent(f.Observed), percent(f.Limit))
	case sc.RulePersistentSlowDown:
		return fmt.Sprintf("o alerta continuou depois de %.0f ondas no tamanho mínimo", f.Observed)
	case sc.RuleHealthUnavailable:
		return "não foi possível ler a saúde do número e do modelo na Meta"
	case sc.RuleMonthlyCap:
		return "o limite mensal de envios foi atingido"
	case sc.RuleOutsideSendHours:
		return "fora do horário de envio"
	case sc.RuleSendHoursInvalid:
		return "o horário de envio da campanha é inválido"
	case sc.RuleDailyWaveLimit:
		return "o limite diário de ondas foi atingido"
	default:
		return string(f.Rule)
	}
}

func percent(v float64) string {
	return strings.Replace(fmt.Sprintf("%.1f%%", v*100), ".", ",", 1)
}
