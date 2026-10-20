package report_renderers

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/report"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	report_xlsx "vozko/usecases/report/xlsx"
)

const (
	formReceivedLayout = "2006-01-02 15:04:05"
	formOtherJoiner    = " | "
)

var (
	errFormExportForm   = errors.New("form responses report: the export names no form")
	errFormExportPeriod = errors.New("form responses report: the period is neither a day nor an instant")

	formValidationScope = wf.ReadScope{Resolved: true, Restrict: true}
)

type WhatsAppFormResponsesParams struct {
	FormID     string   `json:"formId"`
	From       string   `json:"from,omitempty"`
	To         string   `json:"to,omitempty"`
	Origins    []string `json:"origins,omitempty"`
	PhoneIDs   []string `json:"phoneIds,omitempty"`
	VersionIDs []string `json:"versionIds,omitempty"`
	CampaignID string   `json:"campaignId,omitempty"`
	Match      string   `json:"match,omitempty"`
}

func formPeriodBound(raw string, endOfDay bool) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	at := shared.ParseDateBound(raw, endOfDay)
	if at == nil {
		return nil, fmt.Errorf("%w: %q", errFormExportPeriod, raw)
	}
	return at, nil
}

func (p WhatsAppFormResponsesParams) query(workspaceID string, scope wf.ReadScope) (wf.ResponseQuery, error) {
	formID := strings.TrimSpace(p.FormID)
	if _, err := uuid.Parse(formID); err != nil {
		return wf.ResponseQuery{}, errFormExportForm
	}
	from, err := formPeriodBound(p.From, false)
	if err != nil {
		return wf.ResponseQuery{}, err
	}
	to, err := formPeriodBound(p.To, true)
	if err != nil {
		return wf.ResponseQuery{}, err
	}
	origins := make([]wf.SendOrigin, 0, len(p.Origins))
	for _, origin := range p.Origins {
		origins = append(origins, wf.SendOrigin(origin))
	}
	q := wf.ResponseQuery{
		WorkspaceID: workspaceID, FormID: formID, VersionIDs: p.VersionIDs, From: from, To: to, Origins: origins,
		PhoneIDs: p.PhoneIDs, CampaignID: strings.TrimSpace(p.CampaignID), Match: wf.MatchFilter(p.Match),
		Limit: wf.MaxPageSize, Scope: scope,
	}
	if err := q.Normalize(); err != nil {
		return wf.ResponseQuery{}, err
	}
	return q, nil
}

func formResponsesParams(job report.Job) (WhatsAppFormResponsesParams, error) {
	var params WhatsAppFormResponsesParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return WhatsAppFormResponsesParams{}, fmt.Errorf("form responses report: reading parameters: %w", err)
	}
	return params, nil
}

type FormGetter interface {
	Get(ctx context.Context, workspaceID, formID string) (*wf.Form, error)
}

type FormVersionLister interface {
	List(ctx context.Context, workspaceID, formID string) ([]wf.Version, error)
}

type FormResponseLister interface {
	List(ctx context.Context, q wf.ResponseQuery) (wf.Page[wf.Response], error)
}

type FormReadScopes interface {
	ReadScope(userID, workspaceID string, isAdmin bool) (wf.ReadScope, bool)
}

type FormLeadNames interface {
	Names(ctx context.Context, workspaceID string, ids []string) (map[string]string, error)
}

type FormPhones interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type WorkspaceLocations interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

type WhatsAppFormResponsesDeps struct {
	Forms       FormGetter
	Versions    FormVersionLister
	Responses   FormResponseLister
	Scopes      FormReadScopes
	LeadNames   FormLeadNames
	Phones      FormPhones
	Locations   WorkspaceLocations
	Permissions workspace.PermissionChecker
}

func (d WhatsAppFormResponsesDeps) complete() bool {
	return d.Forms != nil && d.Versions != nil && d.Responses != nil && d.Scopes != nil &&
		d.LeadNames != nil && d.Phones != nil && d.Locations != nil && d.Permissions != nil
}

type WhatsAppFormResponsesRenderer struct {
	deps WhatsAppFormResponsesDeps
}

func NewWhatsAppFormResponsesRenderer(deps WhatsAppFormResponsesDeps) *WhatsAppFormResponsesRenderer {
	return &WhatsAppFormResponsesRenderer{deps: deps}
}

func (r *WhatsAppFormResponsesRenderer) Kind() report.Kind { return report.KindWhatsAppFormResponses }

func (r *WhatsAppFormResponsesRenderer) Formats() []report.Format {
	return []report.Format{report.FormatCSV, report.FormatXLSX}
}

func (r *WhatsAppFormResponsesRenderer) Policy(report.Job) (report.Policy, error) {
	required, err := workspace.CapabilitiesRequire([]workspace.CapabilityKey{wf.CapabilityExport})
	if err != nil {
		return report.Policy{}, fmt.Errorf("%w: %w", report.ErrNoPolicy, err)
	}
	return report.Policy{Required: required, RequesterOnly: true}, nil
}

func (r *WhatsAppFormResponsesRenderer) CheckParams(job report.Job) error {
	params, err := formResponsesParams(job)
	if err != nil {
		return err
	}
	_, err = params.query(job.WorkspaceID, formValidationScope)
	return err
}

func (r *WhatsAppFormResponsesRenderer) Render(ctx context.Context, job report.Job, progress report.ProgressFunc) (report.Artifact, error) {
	if !r.deps.complete() {
		return report.Artifact{}, report.ErrNoRenderer
	}
	params, err := formResponsesParams(job)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("%w: %v", report.ErrInvalidParams, err)
	}
	scope, allowed := r.deps.Scopes.ReadScope(job.RequestedBy, job.WorkspaceID, job.RequestedByAdmin)
	if !allowed {
		return report.Artifact{}, fmt.Errorf("%w: the department scope of the requester cannot be resolved", report.ErrNotAllowed)
	}
	scope.Resolved = true
	q, err := params.query(job.WorkspaceID, scope)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("%w: %v", report.ErrInvalidParams, err)
	}
	form, err := r.deps.Forms.Get(ctx, job.WorkspaceID, q.FormID)
	if errors.Is(err, wf.ErrFormNotFound) {
		return report.Artifact{}, fmt.Errorf("%w: %v", report.ErrInvalidParams, err)
	}
	if err != nil {
		return report.Artifact{}, fmt.Errorf("form responses report: the form: %w", err)
	}
	versions, err := r.deps.Versions.List(ctx, job.WorkspaceID, q.FormID)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("form responses report: the versions of the form: %w", err)
	}
	loc, err := r.deps.Locations.Location(ctx, job.WorkspaceID, "")
	if err != nil {
		return report.Artifact{}, fmt.Errorf("form responses report: the time zone of the workspace: %w", err)
	}
	reveal := workspace.HoldsCapability(r.deps.Permissions, job.WorkspaceID, job.RequestedBy, job.RequestedByAdmin, wf.CapabilityResponsesSensitive)
	t := formResponseLabels().For(job.Locale, "formResponses")
	sheet := newFormSheet(t, versions, q.VersionIDs, loc, reveal)
	out, err := newFormTable(job.Format, t("sheet"), sheet.header())
	if err != nil {
		return report.Artifact{}, err
	}
	defer out.discard()
	progress(5)

	phones := map[string]string{}
	rows := 0
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return report.Artifact{}, err
		}
		got, err := r.deps.Responses.List(ctx, q)
		if err != nil {
			return report.Artifact{}, fmt.Errorf("form responses report: a page of responses: %w", err)
		}
		written, err := r.writePage(ctx, out, sheet, job.WorkspaceID, got.Items, phones)
		if err != nil {
			return report.Artifact{}, err
		}
		rows += written
		progress(min(90, 5+page*5))
		if got.Next == "" || len(got.Items) == 0 {
			break
		}
		q.After = got.Next
	}
	if rows == 0 {
		return report.Artifact{}, report.ErrEmptyResult
	}
	data, err := out.finish()
	if err != nil {
		return report.Artifact{}, err
	}
	progress(95)
	return report.Artifact{
		Data:        data,
		ContentType: job.Format.ContentType(),
		Filename:    report.Filename(string(job.Format), "respostas", form.Name, time.Now().In(loc).Format("2006-01-02")),
		RowCount:    int64(rows),
	}, nil
}

func (r *WhatsAppFormResponsesRenderer) writePage(ctx context.Context, out formTable, sheet formSheet, workspaceID string, responses []wf.Response, phones map[string]string) (int, error) {
	var leadIDs []string
	for _, resp := range responses {
		if resp.LeadID != "" && !slices.Contains(leadIDs, resp.LeadID) {
			leadIDs = append(leadIDs, resp.LeadID)
		}
		if err := r.lookUpPhone(phones, resp.BusinessPhoneID); err != nil {
			return 0, err
		}
	}
	names := map[string]string{}
	if len(leadIDs) > 0 {
		found, err := r.deps.LeadNames.Names(ctx, workspaceID, leadIDs)
		if err != nil {
			return 0, fmt.Errorf("form responses report: the lead names of a page: %w", err)
		}
		names = found
	}
	for i, resp := range responses {
		values, err := sheet.row(resp, names[resp.LeadID], phones[resp.BusinessPhoneID])
		if err != nil {
			return i, err
		}
		if err := out.row(values); err != nil {
			return i, err
		}
	}
	return len(responses), nil
}

func (r *WhatsAppFormResponsesRenderer) lookUpPhone(phones map[string]string, phoneID string) error {
	if _, known := phones[phoneID]; known || phoneID == "" {
		return nil
	}
	phone, err := r.deps.Phones.FindByID(phoneID)
	switch {
	case errors.Is(err, businessphone.ErrPhoneNumberNotFound):
		phones[phoneID] = ""
	case err != nil:
		return fmt.Errorf("form responses report: the number %s: %w", phoneID, err)
	case phone != nil:
		phones[phoneID] = phone.DisplayPhoneNumber
	default:
		phones[phoneID] = ""
	}
	return nil
}

type rangePart int

const (
	wholeAnswer rangePart = iota
	rangeStart
	rangeEnd
)

type formColumn struct {
	key   string
	label string
	part  rangePart
}

type formSheet struct {
	t         Translate
	columns   []formColumn
	documents map[string]wf.Document
	numbers   map[string]int
	loc       *time.Location
	reveal    bool
}

func newFormSheet(t Translate, versions []wf.Version, chosen []string, loc *time.Location, reveal bool) formSheet {
	sheet := formSheet{t: t, documents: map[string]wf.Document{}, numbers: map[string]int{}, loc: loc, reveal: reveal}
	for _, v := range versions {
		sheet.documents[v.ID], sheet.numbers[v.ID] = v.Document, v.Number
	}
	newestFirst := slices.Clone(versions)
	slices.SortStableFunc(newestFirst, func(a, b wf.Version) int { return cmp.Compare(b.Number, a.Number) })
	seen := map[string]bool{}
	for _, v := range newestFirst {
		if len(chosen) > 0 && !slices.Contains(chosen, v.ID) {
			continue
		}
		for _, ref := range v.Document.Fields() {
			key := ref.Field.Key
			if seen[key] {
				continue
			}
			seen[key] = true
			label := cmp.Or(ref.Field.Prompt(), key)
			if ref.Field.Kind == wf.FieldDateRange {
				sheet.columns = append(sheet.columns,
					formColumn{key: key, label: label + " (" + t("rangeStart") + ")", part: rangeStart},
					formColumn{key: key, label: label + " (" + t("rangeEnd") + ")", part: rangeEnd})
				continue
			}
			sheet.columns = append(sheet.columns, formColumn{key: key, label: label})
		}
	}
	return sheet
}

func (s formSheet) header() []string {
	header := []string{s.t("receivedAt"), s.t("contact"), s.t("number"), s.t("lead"), s.t("origin"), s.t("campaign"), s.t("version")}
	for _, c := range s.columns {
		header = append(header, c.label)
	}
	return append(header, s.t("match"), s.t("other"))
}

func (s formSheet) row(resp wf.Response, leadName, phone string) ([]string, error) {
	doc, known := s.documents[resp.VersionID]
	if resp.VersionID != "" && !known {
		return nil, fmt.Errorf("form responses report: response %s answers version %s, which the form does not list", resp.ID, resp.VersionID)
	}
	answers := resp.Answers
	if !s.reveal {
		answers = wf.MaskSensitive(doc, answers)
	}
	version, origin := "", ""
	if known {
		version = strconv.Itoa(s.numbers[resp.VersionID])
	}
	if resp.Origin != "" {
		origin = s.t("origin." + string(resp.Origin))
	}
	values := []string{
		resp.SubmittedAt.In(s.loc).Format(formReceivedLayout), resp.ContactNumber, phone, leadName, origin,
		cmp.Or(resp.CampaignID, s.t("unavailable")), version,
	}
	for _, c := range s.columns {
		values = append(values, formCell(doc, answers, c))
	}
	return append(values, s.t("match."+string(resp.Match)), strings.Join(resp.Answers.UnknownLines(), formOtherJoiner)), nil
}

func formCell(doc wf.Document, answers wf.Answers, c formColumn) string {
	answer, answered := answers.Values[c.key]
	ref, inVersion := doc.Field(c.key)
	if !answered || !inVersion {
		return ""
	}
	if answer.Masked {
		return answer.Text
	}
	if answer.Range != nil && c.part != wholeAnswer {
		start, end := answer.Range.Shown()
		if c.part == rangeStart {
			return start
		}
		return end
	}
	if c.part == rangeEnd {
		return ""
	}
	return answer.Display(ref.Field)
}

type formTable interface {
	row(values []string) error
	finish() ([]byte, error)
	discard()
}

func newFormTable(format report.Format, sheetName string, header []string) (formTable, error) {
	switch format {
	case report.FormatCSV:
		table := &csvFormTable{}
		table.buffer.WriteString(report.UTF8BOM)
		if err := table.row(header); err != nil {
			return nil, err
		}
		return table, nil
	case report.FormatXLSX:
		sheet, err := report_xlsx.NewSheet(sheetName, safeTexts(header))
		if err != nil {
			return nil, err
		}
		return &xlsxFormTable{sheet: sheet}, nil
	}
	return nil, report.ErrFormatUnsupported
}

func safeTexts(values []string) []string {
	safe := make([]string, len(values))
	for i, value := range values {
		safe[i] = report.SafeCSVText(value)
	}
	return safe
}

type csvFormTable struct {
	buffer bytes.Buffer
}

func (c *csvFormTable) row(values []string) error {
	cells := make([]report.CSVCell, len(values))
	for i, value := range values {
		cells[i] = report.Text(value)
	}
	return report.WriteCSVRow(&c.buffer, cells)
}

func (c *csvFormTable) finish() ([]byte, error) { return c.buffer.Bytes(), nil }

func (c *csvFormTable) discard() {}

type xlsxFormTable struct {
	sheet *report_xlsx.Sheet
}

func (x *xlsxFormTable) row(values []string) error { return x.sheet.Append(safeTexts(values)) }

func (x *xlsxFormTable) finish() ([]byte, error) {
	var buffer bytes.Buffer
	if err := x.sheet.Write(&buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (x *xlsxFormTable) discard() { x.sheet.Discard() }

func formResponseLabels() *StaticLabels {
	return NewStaticLabels("pt").
		Add("pt", Labels{"formResponses": {
			"sheet": "Respostas", "receivedAt": "Recebida em", "contact": "Contato", "number": "Número", "lead": "Lead", "origin": "Origem",
			"campaign": "Campanha", "version": "Versão", "match": "Correspondência", "other": "Outras respostas",
			"rangeStart": "início", "rangeEnd": "fim", "unavailable": "n/d",
			"origin.inbox": "Conversa", "origin.template_start": "Início de conversa", "origin.template_scheduled": "Modelo agendado",
			"origin.template_bulk": "Envio em massa", "origin.template": "Modelo", "origin.campaign": "Campanha",
			"origin.workflow": "Automação", "origin.elo": "Elo",
			"match.token": "Pelo token do envio", "match.context": "Pela mensagem respondida",
			"match.unmatched_no_token": "Sem correspondência: sem token", "match.unmatched_unknown_token": "Sem correspondência: token desconhecido",
			"match.unmatched_sender": "Sem correspondência: outro remetente",
		}}).
		Add("en", Labels{"formResponses": {
			"sheet": "Responses", "receivedAt": "Received at", "contact": "Contact", "number": "Number", "lead": "Lead", "origin": "Origin",
			"campaign": "Campaign", "version": "Version", "match": "Match", "other": "Other answers",
			"rangeStart": "start", "rangeEnd": "end", "unavailable": "n/a",
			"origin.inbox": "Conversation", "origin.template_start": "Conversation start", "origin.template_scheduled": "Scheduled template",
			"origin.template_bulk": "Bulk send", "origin.template": "Template", "origin.campaign": "Campaign",
			"origin.workflow": "Automation", "origin.elo": "Elo",
			"match.token": "By the send token", "match.context": "By the replied message",
			"match.unmatched_no_token": "Unmatched: no token", "match.unmatched_unknown_token": "Unmatched: unknown token",
			"match.unmatched_sender": "Unmatched: another sender",
		}}).
		Add("es", Labels{"formResponses": {
			"sheet": "Respuestas", "receivedAt": "Recibida el", "contact": "Contacto", "number": "Número", "lead": "Lead", "origin": "Origen",
			"campaign": "Campaña", "version": "Versión", "match": "Correspondencia", "other": "Otras respuestas",
			"rangeStart": "inicio", "rangeEnd": "fin", "unavailable": "n/d",
			"origin.inbox": "Conversación", "origin.template_start": "Inicio de conversación", "origin.template_scheduled": "Plantilla programada",
			"origin.template_bulk": "Envío masivo", "origin.template": "Plantilla", "origin.campaign": "Campaña",
			"origin.workflow": "Automatización", "origin.elo": "Elo",
			"match.token": "Por el token del envío", "match.context": "Por el mensaje respondido",
			"match.unmatched_no_token": "Sin correspondencia: sin token", "match.unmatched_unknown_token": "Sin correspondencia: token desconocido",
			"match.unmatched_sender": "Sin correspondencia: otro remitente",
		}}).
		Add("de", Labels{"formResponses": {
			"sheet": "Antworten", "receivedAt": "Empfangen am", "contact": "Kontakt", "number": "Nummer", "lead": "Lead", "origin": "Herkunft",
			"campaign": "Kampagne", "version": "Version", "match": "Zuordnung", "other": "Weitere Antworten",
			"rangeStart": "Beginn", "rangeEnd": "Ende", "unavailable": "k. A.",
			"origin.inbox": "Unterhaltung", "origin.template_start": "Gesprächsbeginn", "origin.template_scheduled": "Geplante Vorlage",
			"origin.template_bulk": "Massenversand", "origin.template": "Vorlage", "origin.campaign": "Kampagne",
			"origin.workflow": "Automatisierung", "origin.elo": "Elo",
			"match.token": "Über das Versand-Token", "match.context": "Über die beantwortete Nachricht",
			"match.unmatched_no_token": "Keine Zuordnung: kein Token", "match.unmatched_unknown_token": "Keine Zuordnung: unbekanntes Token",
			"match.unmatched_sender": "Keine Zuordnung: anderer Absender",
		}})
}

var (
	_ report.Guard         = (*WhatsAppFormResponsesRenderer)(nil)
	_ report.ParamsChecker = (*WhatsAppFormResponsesRenderer)(nil)
)
