package googlesheet

import (
	"context"

	"vozko/domain/googlesheet"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

var attentionCopy = map[googlesheet.AttentionReason]string{
	googlesheet.ReasonHeaderMismatch:    "O cabeçalho da planilha foi alterado no Google. Para não gravar dados na coluna errada, as novas linhas ficam guardadas até alguém reparar o cabeçalho.",
	googlesheet.ReasonAccessLost:        "A conta conectada perdeu o acesso à planilha no Google. As novas linhas ficam guardadas até o acesso voltar.",
	googlesheet.ReasonSpreadsheetGone:   "A planilha ou a aba foi removida ou renomeada no Google. As novas linhas ficam guardadas até alguém revisar a planilha.",
	googlesheet.ReasonSheetFull:         "A planilha está perto do limite de 10 milhões de células do Google. Crie uma nova planilha com as mesmas colunas.",
	googlesheet.ReasonGoogleUnavailable: "O Google não aceitou as gravações depois de várias tentativas. As linhas ficam guardadas para tentar de novo.",
	googlesheet.ReasonAccountChanged:    "A conta Google conectada ao Planilhas mudou e não tem acesso a esta planilha. Conecte de novo a conta que criou a planilha.",
}

type alerts struct {
	publisher notification.EpisodePublisher
}

func NewAlerts(publisher notification.EpisodePublisher) Alerts {
	if publisher == nil {
		panic("googlesheet: alerts require the notification publisher")
	}
	return &alerts{publisher: publisher}
}

func attentionEpisodeKey(sheetID string) string { return "google_sheet_attention:" + sheetID }

func (a *alerts) NeedsAttention(ctx context.Context, sheet *googlesheet.Sheet) error {
	if sheet == nil || sheet.Status != googlesheet.StatusNeedsAttention {
		return nil
	}
	body, ok := attentionCopy[sheet.Reason]
	if !ok {
		body = attentionCopy[googlesheet.ReasonGoogleUnavailable]
	}
	if sheet.ReasonDetail != "" {
		body += " Detalhe: " + sheet.ReasonDetail + "."
	}
	_, err := a.publisher.Publish(ctx, notification.Draft{
		WorkspaceID: sheet.WorkspaceID,
		Category:    notification.CategoryAutomations,
		Severity:    notification.SeverityWarning,
		Title:       notification.ClipTitle("A planilha " + sheet.Title + " precisa de atenção"),
		Body:        body,
		ActionURL:   sheet.DashboardLink(),
		ActionLabel: "Abrir planilha",
		Audience:    notification.Holding(workspace.ResourceGoogleSheets, workspace.ActionUpdate),
		Email:       true,
		EpisodeKey:  attentionEpisodeKey(sheet.ID),
		Source:      notification.SourceGoogleSheet,
	})
	return err
}

func (a *alerts) Resolved(ctx context.Context, sheet *googlesheet.Sheet) error {
	if sheet == nil {
		return nil
	}
	return a.publisher.Resolve(ctx, sheet.WorkspaceID, attentionEpisodeKey(sheet.ID))
}
