package report_renderers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"vozko/domain/report"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/domain/workspace"
)

type WhatsAppDispatchParams struct {
	DateFrom     string `json:"dateFrom"`
	DateTo       string `json:"dateTo"`
	CampaignID   string `json:"campaignId,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
}

func (p WhatsAppDispatchParams) period() wc.ReportPeriod {
	return wc.ReportPeriod{DateFrom: p.DateFrom, DateTo: p.DateTo}
}

type WhatsAppDispatchRenderer struct {
	exporter wc.ExportDispatchReportUseCase
	scopes   DepartmentScopes
	now      func() time.Time
}

func NewWhatsAppDispatchRenderer(exporter wc.ExportDispatchReportUseCase, scopes DepartmentScopes) *WhatsAppDispatchRenderer {
	return &WhatsAppDispatchRenderer{exporter: exporter, scopes: scopes, now: func() time.Time { return time.Now().UTC() }}
}

func (r *WhatsAppDispatchRenderer) Kind() report.Kind { return report.KindWhatsAppDispatch }

func (r *WhatsAppDispatchRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV}
}

func (r *WhatsAppDispatchRenderer) Policy(report.Job) (report.Policy, error) {
	return report.Policy{
		Required: []workspace.PermissionEntry{
			{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead},
			{Resource: workspace.ResourceWhatsAppCampaigns, Action: workspace.ActionRead},
		},
		RequesterOnly: true,
	}, nil
}

func (r *WhatsAppDispatchRenderer) CheckParams(job report.Job) error {
	params, err := dispatchParams(job)
	if err != nil {
		return err
	}
	_, err = wce.NewDayWindowWithin(params.DateFrom, params.DateTo, time.UTC, wce.MaxExportDays)
	return err
}

func dispatchParams(job report.Job) (WhatsAppDispatchParams, error) {
	var params WhatsAppDispatchParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return WhatsAppDispatchParams{}, fmt.Errorf("dispatch report: reading parameters: %w", err)
	}
	return params, nil
}

func (r *WhatsAppDispatchRenderer) Render(ctx context.Context, job report.Job, progress report.ProgressFunc) (report.Artifact, error) {
	if r.exporter == nil || r.scopes == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}
	params, err := dispatchParams(job)
	if err != nil {
		return report.Artifact{}, err
	}
	scope, err := r.scopes.For(ctx, job.WorkspaceID, job.RequestedBy, job.RequestedByAdmin)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("dispatch report: the department scope of the requester: %w", err)
	}
	access := wc.ScopedReportAccess(job.WorkspaceID, params.CampaignID, params.DepartmentID, scope)

	var buffer bytes.Buffer
	buffer.WriteString(report.UTF8BOM)
	sheet := &dispatchSheet{
		w:           &buffer,
		t:           dispatchExportLabels().For(job.Locale, dispatchLabelNamespace),
		params:      params,
		generatedAt: r.now(),
		progress:    progress,
	}
	if err := r.exporter.Export(ctx, access, params.period(), sheet); err != nil {
		return report.Artifact{}, dispatchFailure(err)
	}
	if err := sheet.finish(); err != nil {
		return report.Artifact{}, err
	}
	return report.Artifact{
		Data:        buffer.Bytes(),
		ContentType: report.FormatCSV.ContentType(),
		Filename:    report.Filename("csv", "disparos", params.DateFrom, params.DateTo),
		RowCount:    sheet.rows,
	}, nil
}

func dispatchFailure(err error) error {
	switch {
	case errors.Is(err, wc.ErrCampaignAccessDenied):
		return fmt.Errorf("%w: %v", report.ErrNotAllowed, err)
	case errors.Is(err, wc.ErrCampaignNotFound):
		return fmt.Errorf("%w: %v", report.ErrEmptyResult, err)
	case errors.Is(err, wce.ErrReportWindowInvalid), errors.Is(err, wce.ErrReportWindowTooLong):
		return fmt.Errorf("%w: %v", report.ErrInvalidParams, err)
	}
	return err
}

const (
	dispatchLabelNamespace = "whatsappDispatch"
	campaignsProgressStart = 30
	campaignsProgressEnd   = 80
	campaignsProgressStep  = 5
)

type dispatchSheet struct {
	w           io.Writer
	t           Translate
	params      WhatsAppDispatchParams
	generatedAt time.Time
	progress    report.ProgressFunc

	funnel        wce.Funnel
	rows          int64
	sections      int
	footer        []report.CSVCell
	campaignPages int
	tagsStarted   bool
}

func (s *dispatchSheet) Summary(summary *wc.ReportSummary) error {
	if summary.Funnel.Base == 0 {
		return report.ErrEmptyResult
	}
	s.funnel = summary.Funnel
	if err := s.writeFilters(summary); err != nil {
		return err
	}
	if err := s.writeFunnel(); err != nil {
		return err
	}
	s.progress(15)
	return nil
}

func (s *dispatchSheet) writeFilters(summary *wc.ReportSummary) error {
	campaign := summary.CampaignName
	if summary.CampaignID == "" {
		campaign = s.t("allCampaigns")
	}
	if err := s.beginSection("title"); err != nil {
		return err
	}
	rows := [][]report.CSVCell{
		metricRow(s.t, "dateFrom", report.Text(s.params.DateFrom)),
		metricRow(s.t, "dateTo", report.Text(s.params.DateTo)),
		metricRow(s.t, "campaign", report.Text(campaign)),
	}
	if summary.TemplateName != "" {
		rows = append(rows, metricRow(s.t, "template", report.Text(summary.TemplateName)))
	}
	rows = append(rows, metricRow(s.t, "generatedAt", report.Text(s.generatedAt.Format(isoMillis))))
	return s.writeRows(rows)
}

func (s *dispatchSheet) writeFunnel() error {
	f := s.funnel
	if err := s.beginSection("funnel", "stage", "count", "shareOfBase"); err != nil {
		return err
	}
	stages := []struct {
		key   string
		value int64
	}{
		{"base", f.Base}, {"sent", f.Sent}, {"delivered", f.Delivered}, {"read", f.Read}, {"replied", f.Replied},
		{"failed", f.Failed}, {"awaitingDelivery", f.AwaitingDelivery}, {"pending", f.Pending}, {"notEligible", f.NotEligible},
	}
	rows := make([][]report.CSVCell, 0, len(stages)+1)
	for _, stage := range stages {
		rows = append(rows, []report.CSVCell{report.Text(s.t(stage.key)), report.Int(stage.value), pctCell(stage.value, f.Base)})
	}
	tracked := report.Empty()
	if f.TrackedSince != nil {
		tracked = report.Text(f.TrackedSince.UTC().Format(isoMillis))
	}
	rows = append(rows, metricRow(s.t, "trackedSince", tracked))
	return s.writeRows(rows)
}

func (s *dispatchSheet) Daily(daily *wc.ReportDaily) error {
	if err := s.beginSection("daily"); err != nil {
		return err
	}
	if err := s.writeRow(metricRow(s.t, "timezone", report.Text(daily.Timezone))); err != nil {
		return err
	}
	if err := s.writeHeader("day", "sent", "delivered", "read", "replied"); err != nil {
		return err
	}
	for _, day := range daily.Days {
		if err := s.writeRow([]report.CSVCell{report.Text(day.Day), report.Int(day.Sent), report.Int(day.Delivered), report.Int(day.Read), report.Int(day.Replied)}); err != nil {
			return err
		}
	}
	s.progress(campaignsProgressStart)
	return nil
}

func (s *dispatchSheet) Campaigns(rows []wce.CampaignFunnel) error {
	if s.campaignPages == 0 {
		if err := s.beginSection("campaigns", "campaignName", "base", "sent", "delivered", "deliveryRate", "read", "readRate", "replied", "replyRate", "failed"); err != nil {
			return err
		}
		s.footer = campaignCells(s.t("total"), s.funnel.Base, s.funnel.Sent, s.funnel.Delivered, s.funnel.Read, s.funnel.Replied, s.funnel.Failed)
	}
	s.campaignPages++
	for _, row := range rows {
		if err := s.writeRow(campaignCells(row.CampaignName, row.Base, row.Sent, row.Delivered, row.Read, row.Replied, row.Failed)); err != nil {
			return err
		}
	}
	s.progress(min(campaignsProgressEnd, campaignsProgressStart+s.campaignPages*campaignsProgressStep))
	return nil
}

func campaignCells(name string, base, sent, delivered, read, replied, failed int64) []report.CSVCell {
	return []report.CSVCell{
		report.Text(name), report.Int(base), report.Int(sent),
		report.Int(delivered), pctCell(delivered, sent),
		report.Int(read), pctCell(read, delivered),
		report.Int(replied), pctCell(replied, read),
		report.Int(failed),
	}
}

func (s *dispatchSheet) Failures(failures *wc.ReportFailures) error {
	if err := s.beginSection("failures", "errorCode", "count", "shareOfFailures"); err != nil {
		return err
	}
	for _, reason := range failures.Reasons {
		if err := s.writeRow([]report.CSVCell{report.Int(int64(reason.Code)), report.Int(reason.Count), pctCell(reason.Count, s.funnel.Failed)}); err != nil {
			return err
		}
	}
	s.progress(85)
	return nil
}

func (s *dispatchSheet) Tags(rows []wce.TagCount) error {
	if !s.tagsStarted {
		if err := s.beginSection("tags", "tag", "count", "shareOfBase"); err != nil {
			return err
		}
		s.tagsStarted = true
	}
	for _, tag := range rows {
		if err := s.writeRow([]report.CSVCell{report.Text(tag.Name), report.Int(tag.Count), pctCell(tag.Count, s.funnel.Base)}); err != nil {
			return err
		}
	}
	s.progress(95)
	return nil
}

func (s *dispatchSheet) beginSection(titleKey string, header ...string) error {
	if err := s.closeSection(); err != nil {
		return err
	}
	if s.sections > 0 {
		if _, err := io.WriteString(s.w, report.CSVNewline); err != nil {
			return err
		}
	}
	s.sections++
	if err := report.WriteCSVRow(s.w, []report.CSVCell{report.Text(s.t(titleKey))}); err != nil {
		return err
	}
	if len(header) == 0 {
		return nil
	}
	return s.writeHeader(header...)
}

func (s *dispatchSheet) writeHeader(keys ...string) error {
	cells := make([]report.CSVCell, 0, len(keys))
	for _, key := range keys {
		cells = append(cells, report.Text(s.t(key)))
	}
	return report.WriteCSVRow(s.w, cells)
}

func (s *dispatchSheet) closeSection() error {
	if s.footer == nil {
		return nil
	}
	footer := s.footer
	s.footer = nil
	return s.writeRow(footer)
}

func (s *dispatchSheet) finish() error {
	return s.closeSection()
}

func (s *dispatchSheet) writeRows(rows [][]report.CSVCell) error {
	for _, row := range rows {
		if err := s.writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (s *dispatchSheet) writeRow(cells []report.CSVCell) error {
	if err := report.WriteCSVRow(s.w, cells); err != nil {
		return err
	}
	s.rows++
	return nil
}

func dispatchExportLabels() *StaticLabels {
	return NewStaticLabels("pt").
		Add("pt", Labels{dispatchLabelNamespace: {
			"title": "Disparo de campanhas", "dateFrom": "Data inicial", "dateTo": "Data final", "campaign": "Campanha",
			"allCampaigns": "Todas as campanhas", "template": "Modelo", "generatedAt": "Gerado em",
			"funnel": "Funil", "stage": "Etapa", "count": "Quantidade", "shareOfBase": "% dos contatos",
			"base": "Contatos", "sent": "Enviadas", "delivered": "Entregues", "read": "Lidas", "replied": "Respondidas",
			"failed": "Falhas", "awaitingDelivery": "Aguardando entrega", "pending": "Na fila", "notEligible": "Não elegíveis",
			"trackedSince": "Acompanhado desde", "daily": "Por dia", "timezone": "Fuso horário", "day": "Dia",
			"campaigns": "Por campanha", "campaignName": "Campanha", "deliveryRate": "Taxa de entrega (%)",
			"readRate": "Taxa de leitura (%)", "replyRate": "Taxa de resposta (%)", "total": "Total",
			"failures": "Motivos de falha", "errorCode": "Código do erro", "shareOfFailures": "% das falhas",
			"tags": "Etiquetas", "tag": "Etiqueta",
		}}).
		Add("en", Labels{dispatchLabelNamespace: {
			"title": "Campaign dispatch", "dateFrom": "Start date", "dateTo": "End date", "campaign": "Campaign",
			"allCampaigns": "All campaigns", "template": "Template", "generatedAt": "Generated at",
			"funnel": "Funnel", "stage": "Stage", "count": "Count", "shareOfBase": "% of contacts",
			"base": "Contacts", "sent": "Sent", "delivered": "Delivered", "read": "Read", "replied": "Replied",
			"failed": "Failed", "awaitingDelivery": "Awaiting delivery", "pending": "Queued", "notEligible": "Not eligible",
			"trackedSince": "Tracked since", "daily": "By day", "timezone": "Time zone", "day": "Day",
			"campaigns": "By campaign", "campaignName": "Campaign", "deliveryRate": "Delivery rate (%)",
			"readRate": "Read rate (%)", "replyRate": "Reply rate (%)", "total": "Total",
			"failures": "Failure reasons", "errorCode": "Error code", "shareOfFailures": "% of failures",
			"tags": "Tags", "tag": "Tag",
		}}).
		Add("es", Labels{dispatchLabelNamespace: {
			"title": "Envío de campañas", "dateFrom": "Fecha inicial", "dateTo": "Fecha final", "campaign": "Campaña",
			"allCampaigns": "Todas las campañas", "template": "Plantilla", "generatedAt": "Generado el",
			"funnel": "Embudo", "stage": "Etapa", "count": "Cantidad", "shareOfBase": "% de los contactos",
			"base": "Contactos", "sent": "Enviados", "delivered": "Entregados", "read": "Leídos", "replied": "Respondidos",
			"failed": "Fallos", "awaitingDelivery": "Esperando entrega", "pending": "En cola", "notEligible": "No elegibles",
			"trackedSince": "Seguimiento desde", "daily": "Por día", "timezone": "Zona horaria", "day": "Día",
			"campaigns": "Por campaña", "campaignName": "Campaña", "deliveryRate": "Tasa de entrega (%)",
			"readRate": "Tasa de lectura (%)", "replyRate": "Tasa de respuesta (%)", "total": "Total",
			"failures": "Motivos de fallo", "errorCode": "Código de error", "shareOfFailures": "% de los fallos",
			"tags": "Etiquetas", "tag": "Etiqueta",
		}}).
		Add("de", Labels{dispatchLabelNamespace: {
			"title": "Kampagnenversand", "dateFrom": "Startdatum", "dateTo": "Enddatum", "campaign": "Kampagne",
			"allCampaigns": "Alle Kampagnen", "template": "Vorlage", "generatedAt": "Erstellt am",
			"funnel": "Trichter", "stage": "Stufe", "count": "Anzahl", "shareOfBase": "% der Kontakte",
			"base": "Kontakte", "sent": "Gesendet", "delivered": "Zugestellt", "read": "Gelesen", "replied": "Beantwortet",
			"failed": "Fehlgeschlagen", "awaitingDelivery": "Wartet auf Zustellung", "pending": "In der Warteschlange", "notEligible": "Nicht berechtigt",
			"trackedSince": "Erfasst seit", "daily": "Pro Tag", "timezone": "Zeitzone", "day": "Tag",
			"campaigns": "Pro Kampagne", "campaignName": "Kampagne", "deliveryRate": "Zustellrate (%)",
			"readRate": "Leserate (%)", "replyRate": "Antwortrate (%)", "total": "Gesamt",
			"failures": "Fehlergründe", "errorCode": "Fehlercode", "shareOfFailures": "% der Fehler",
			"tags": "Etiketten", "tag": "Etikett",
		}})
}

var (
	_ report.Guard          = (*WhatsAppDispatchRenderer)(nil)
	_ report.ParamsChecker  = (*WhatsAppDispatchRenderer)(nil)
	_ wc.DispatchExportSink = (*dispatchSheet)(nil)
)
