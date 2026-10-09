package report_renderers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
)

const leadExportPage = 500

type LeadsParams struct {
	SnapshotID string `json:"snapshotId"`
	Addresses  bool   `json:"addresses,omitempty"`
	Sensitive  bool   `json:"sensitive,omitempty"`
	Selected   int    `json:"selected,omitempty"`
}

func (p LeadsParams) Tier() string {
	switch {
	case p.Addresses && p.Sensitive:
		return "addresses+sensitive"
	case p.Addresses:
		return "addresses"
	case p.Sensitive:
		return "sensitive"
	}
	return "basic"
}

type LeadExportSource interface {
	Snapshot(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error)
	AttachContactDetails(ctx context.Context, workspaceID string, leads []*lead.Lead) error
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
	Names(ids ...string) map[string]string
}

type LeadsRenderer struct {
	source LeadExportSource
	page   int
}

func NewLeadsRenderer(source LeadExportSource) *LeadsRenderer {
	return &LeadsRenderer{source: source, page: leadExportPage}
}

func (r *LeadsRenderer) Kind() report.Kind { return report.KindLeads }

func (r *LeadsRenderer) Formats() []report.Format { return []report.Format{report.FormatCSV} }

func (r *LeadsRenderer) Internal() bool { return true }

func leadsParams(job report.Job) (LeadsParams, error) {
	var params LeadsParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return LeadsParams{}, fmt.Errorf("%w: %v", report.ErrNoPolicy, err)
	}
	if strings.TrimSpace(params.SnapshotID) == "" {
		return LeadsParams{}, fmt.Errorf("%w: the export names no frozen selection", report.ErrNoPolicy)
	}
	return params, nil
}

func (r *LeadsRenderer) Policy(job report.Job) (report.Policy, error) {
	params, err := leadsParams(job)
	if err != nil {
		return report.Policy{}, err
	}
	required, err := leadaction.ExportRequirements(params.Addresses, params.Sensitive)
	if err != nil {
		return report.Policy{}, fmt.Errorf("%w: %w", report.ErrNoPolicy, err)
	}
	return report.Policy{Required: required, Tier: params.Tier()}, nil
}

func (r *LeadsRenderer) Render(ctx context.Context, job report.Job, progress report.ProgressFunc) (report.Artifact, error) {
	if r.source == nil {
		return report.Artifact{}, report.ErrNoRenderer
	}
	params, err := leadsParams(job)
	if err != nil {
		return report.Artifact{}, err
	}
	defs, err := r.source.ListByObject(job.WorkspaceID, customfield.ObjectLead)
	if err != nil {
		return report.Artifact{}, fmt.Errorf("leads report: the lead fields: %w", err)
	}
	viewer := lead.Viewer{ReadsLeads: true, ReadsAddresses: params.Addresses, Fields: customfield.Viewer{ReadsSensitive: params.Sensitive}, Definitions: defs}
	sheet := newLeadSheet(leadExportLabels().For(job.Locale, "leadsExport"), viewer, params.Addresses)

	var buffer bytes.Buffer
	buffer.WriteString(report.UTF8BOM)
	if err := report.WriteCSVRow(&buffer, sheet.header()); err != nil {
		return report.Artifact{}, err
	}
	rows, after := 0, ""
	for {
		if err := ctx.Err(); err != nil {
			return report.Artifact{}, err
		}
		ids, err := r.source.Snapshot(ctx, job.WorkspaceID, params.SnapshotID, after, r.page)
		if err != nil {
			return report.Artifact{}, fmt.Errorf("leads report: the frozen selection: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		written, err := r.writePage(ctx, &buffer, job.WorkspaceID, ids, sheet)
		if err != nil {
			return report.Artifact{}, err
		}
		rows += written
		after = ids[len(ids)-1]
		if params.Selected > 0 {
			progress(min(95, 5+rows*90/params.Selected))
		}
		if len(ids) < r.page {
			break
		}
	}
	if rows == 0 {
		return report.Artifact{}, report.ErrEmptyResult
	}
	return report.Artifact{
		Data:        buffer.Bytes(),
		ContentType: report.FormatCSV.ContentType(),
		Filename:    report.Filename("csv", "leads", time.Now().UTC().Format("2006-01-02")),
		RowCount:    int64(rows),
	}, nil
}

func (r *LeadsRenderer) writePage(ctx context.Context, w io.Writer, workspaceID string, ids []string, sheet leadSheet) (int, error) {
	leads, err := r.source.FindByIDs(workspaceID, ids)
	if err != nil {
		return 0, fmt.Errorf("leads report: the leads of a page: %w", err)
	}
	if err := r.source.AttachContactDetails(ctx, workspaceID, leads); err != nil {
		return 0, fmt.Errorf("leads report: the phones and addresses of a page: %w", err)
	}
	byID := make(map[string]*lead.Lead, len(leads))
	owners := make([]string, 0, len(leads))
	for _, l := range leads {
		if l == nil {
			continue
		}
		byID[l.ID] = l
		if l.Owner != "" {
			owners = append(owners, l.Owner)
		}
	}
	names := map[string]string{}
	if len(owners) > 0 {
		names = r.source.Names(owners...)
	}
	written := 0
	for _, id := range ids {
		l, ok := byID[id]
		if !ok {
			continue
		}
		if err := report.WriteCSVRow(w, sheet.row(l, names)); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

type leadSheet struct {
	t         Translate
	viewer    lead.Viewer
	addresses bool
	fields    []*customfield.Definition
}

func newLeadSheet(t Translate, viewer lead.Viewer, addresses bool) leadSheet {
	fields := make([]*customfield.Definition, 0, len(viewer.Definitions))
	for _, def := range viewer.Definitions {
		if customfield.VisibleTo(def, viewer.Fields) {
			fields = append(fields, def)
		}
	}
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Position < fields[j].Position })
	return leadSheet{t: t, viewer: viewer, addresses: addresses, fields: fields}
}

func (s leadSheet) header() []report.CSVCell {
	keys := []string{"id", "name", "number", "phones", "email", "nickname", "birthDate", "owner", "district", "city", "state"}
	if s.addresses {
		keys = append(keys, "zipCode", "street", "streetNumber", "complement")
	}
	keys = append(keys, "blocked", "optedOut", "whatsappOptIn", "createdAt")
	cells := make([]report.CSVCell, 0, len(keys)+len(s.fields))
	for _, key := range keys {
		cells = append(cells, report.Text(s.t(key)))
	}
	for _, def := range s.fields {
		cells = append(cells, report.Text(def.Label))
	}
	return cells
}

func (s leadSheet) row(stored *lead.Lead, names map[string]string) []report.CSVCell {
	l := lead.VisibleFields(stored, s.viewer)
	phones := make([]string, 0, len(l.Phones))
	for _, p := range l.Phones {
		phones = append(phones, p.Number)
	}
	birth := ""
	if l.BirthDate != nil {
		birth = l.BirthDate.String()
	}
	area := lead.Address{}
	if primary := l.PrimaryAddress(); primary != nil {
		area = *primary
	}
	cells := []report.CSVCell{
		report.Text(l.ID), report.Text(l.Name), report.Text(l.Number), report.Text(strings.Join(phones, " | ")),
		report.Text(l.Email), report.Text(l.Nickname), report.Text(birth), report.Text(names[l.Owner]),
		report.Text(area.Postal.District), report.Text(area.Postal.City), report.Text(area.Postal.State),
	}
	if s.addresses {
		cells = append(cells, report.Text(area.Postal.ZipCode), report.Text(area.Postal.Street), report.Text(area.Postal.Number), report.Text(area.Postal.Complement))
	}
	cells = append(cells, report.Bool(l.Blocked), report.Bool(l.OptedOutAt != nil), report.Bool(l.WhatsAppOptIn != nil), report.Text(l.CreatedAt.UTC().Format(isoMillis)))
	for _, def := range s.fields {
		value, ok := l.CustomFields[def.Key]
		if !ok {
			cells = append(cells, report.Empty())
			continue
		}
		cells = append(cells, report.Text(customfield.FormatValue(value)))
	}
	return cells
}

func leadExportLabels() *StaticLabels {
	return NewStaticLabels("pt").
		Add("pt", Labels{"leadsExport": {
			"id": "ID", "name": "Nome", "number": "Número", "phones": "Telefones", "email": "E-mail", "nickname": "Apelido",
			"birthDate": "Data de nascimento", "owner": "Responsável", "district": "Bairro", "city": "Cidade", "state": "UF",
			"zipCode": "CEP", "street": "Logradouro", "streetNumber": "Número do endereço", "complement": "Complemento",
			"blocked": "Bloqueado", "optedOut": "Não quer receber mensagens", "whatsappOptIn": "Consentimento no WhatsApp", "createdAt": "Criado em",
		}}).
		Add("en", Labels{"leadsExport": {
			"id": "ID", "name": "Name", "number": "Number", "phones": "Phones", "email": "Email", "nickname": "Nickname",
			"birthDate": "Birth date", "owner": "Owner", "district": "District", "city": "City", "state": "State",
			"zipCode": "Postal code", "street": "Street", "streetNumber": "Street number", "complement": "Complement",
			"blocked": "Blocked", "optedOut": "Opted out of messages", "whatsappOptIn": "WhatsApp consent", "createdAt": "Created at",
		}}).
		Add("es", Labels{"leadsExport": {
			"id": "ID", "name": "Nombre", "number": "Número", "phones": "Teléfonos", "email": "Correo", "nickname": "Apodo",
			"birthDate": "Fecha de nacimiento", "owner": "Responsable", "district": "Barrio", "city": "Ciudad", "state": "Estado",
			"zipCode": "Código postal", "street": "Calle", "streetNumber": "Número de la dirección", "complement": "Complemento",
			"blocked": "Bloqueado", "optedOut": "No quiere recibir mensajes", "whatsappOptIn": "Consentimiento en WhatsApp", "createdAt": "Creado el",
		}}).
		Add("de", Labels{"leadsExport": {
			"id": "ID", "name": "Name", "number": "Nummer", "phones": "Telefone", "email": "E-Mail", "nickname": "Spitzname",
			"birthDate": "Geburtsdatum", "owner": "Verantwortlich", "district": "Stadtteil", "city": "Stadt", "state": "Bundesstaat",
			"zipCode": "Postleitzahl", "street": "Straße", "streetNumber": "Hausnummer", "complement": "Zusatz",
			"blocked": "Gesperrt", "optedOut": "Möchte keine Nachrichten", "whatsappOptIn": "WhatsApp Einwilligung", "createdAt": "Erstellt am",
		}})
}

var (
	_ report.Guard    = (*LeadsRenderer)(nil)
	_ report.Internal = (*LeadsRenderer)(nil)
)
