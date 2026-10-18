package notification_usecase

import (
	"time"

	"vozko/domain/notification"
)

const (
	GenericEmailTemplate = "notification.html"
	defaultActionLabel   = "Abrir no Vozko"
)

var categoryLabels = map[notification.Category]string{
	notification.CategoryAnnouncements: "Comunicados",
	notification.CategoryCampaigns:     "Campanhas",
	notification.CategoryTemplates:     "Modelos",
	notification.CategoryChannels:      "Canais",
	notification.CategoryBilling:       "Financeiro",
	notification.CategoryAdvertising:   "Anúncios",
	notification.CategoryTeam:          "Equipe",
	notification.CategorySupport:       "Suporte",
	notification.CategorySystem:        "Sistema",
}

var severityTones = map[notification.Severity]string{
	notification.SeverityInfo:     "info",
	notification.SeveritySuccess:  "success",
	notification.SeverityWarning:  "warning",
	notification.SeverityCritical: "danger",
}

var brasilia = loadBrasilia()

func loadBrasilia() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}

type emailComposer struct {
	dashboardURL string
}

func (c emailComposer) compose(rec notification.Record, workspaceName string) notification.EmailCopy {
	if rec.EmailCopy != nil && rec.EmailCopy.Template != "" {
		return *rec.EmailCopy
	}
	placeholders := map[string]interface{}{
		"Headline":      rec.Title,
		"Message":       rec.Body,
		"CategoryLabel": categoryLabels[rec.Category],
		"Tone":          severityTones[rec.Severity],
		"ImageURL":      rec.ImageURL,
		"InboxURL":      c.dashboardURL + "/dashboard",
		"WorkspaceName": workspaceName,
		"SentAt":        rec.CreatedAt.In(brasilia).Format("02/01/2006 15:04"),
	}
	if rec.ActionURL != "" {
		placeholders["ActionURL"] = notification.AbsoluteLink(c.dashboardURL, rec.ActionURL)
		placeholders["ActionLabel"] = rec.ActionLabel
		if rec.ActionLabel == "" {
			placeholders["ActionLabel"] = defaultActionLabel
		}
	}
	return notification.EmailCopy{Template: GenericEmailTemplate, Subject: rec.Title, Placeholders: placeholders}
}
