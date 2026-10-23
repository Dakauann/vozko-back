package campaignaudience_usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/emailaddress"
	"vozko/domain/lead"
)

type Upload struct {
	FileName string
	Data     []byte
	Empty    bool
}

type View struct {
	Params    Params
	Variables []campaignaudience.Variable
}

func (v View) hasContext() bool {
	return strings.TrimSpace(v.Params.TemplateID) != "" || v.Params.Message != nil
}

type PreviewColumns struct {
	Contact   string                           `json:"contact,omitempty"`
	Name      string                           `json:"name,omitempty"`
	Variables []campaign.VariableColumnMapping `json:"variables"`
	Metadata  []campaign.MetadataColumn        `json:"metadata"`
}

type Skipped struct {
	Total    int                              `json:"total"`
	ByReason map[campaign.ListIssueReason]int `json:"byReason"`
	Lines    []campaign.ListIssue             `json:"lines"`
}

type Preview struct {
	ID              string                         `json:"id"`
	Channel         campaignaudience.Channel       `json:"channel"`
	Contact         campaign.ContactKind           `json:"contact"`
	FileName        string                         `json:"fileName"`
	Lines           int                            `json:"lines"`
	Rows            int                            `json:"rows"`
	Existing        int                            `json:"existing"`
	New             int                            `json:"new"`
	Header          bool                           `json:"header"`
	Columns         PreviewColumns                 `json:"columns"`
	VariableColumns int                            `json:"variableColumns"`
	Skipped         Skipped                        `json:"skipped"`
	MetadataMissing []campaignaudience.MetadataGap `json:"metadataMissing"`
	ExpiresAt       *time.Time                     `json:"expiresAt"`
}

func (s *Service) Upload(ctx context.Context, a Actor, ch campaignaudience.Channel, up Upload, view View) (Preview, error) {
	if err := listChannel(ch); err != nil {
		return Preview{}, err
	}
	parsed := campaign.ContactList{Layout: campaignaudience.EmptyLayout()}
	if !up.Empty {
		if len(up.Data) == 0 {
			return Preview{}, campaignaudience.ErrFileRequired
		}
		if len(up.Data) > campaignaudience.MaxFileBytes {
			return Preview{}, campaignaudience.ErrFileTooLarge
		}
		rows, err := s.deps.Sheets.Read(up.Data)
		if err != nil {
			return Preview{}, errors.Join(campaignaudience.ErrFileUnreadable, err)
		}
		if parsed, err = campaign.ReadContactList(rows, ch.Contact()); err != nil {
			return Preview{}, err
		}
	}
	list := campaignaudience.NewList(s.deps.NewID(), a.WorkspaceID, a.UserID, ch, up.FileName, parsed, s.now())
	rows := make([]campaignaudience.Row, 0, len(parsed.Rows))
	for _, r := range parsed.Rows {
		rows = append(rows, campaignaudience.Row{ID: s.deps.NewID(), ListID: list.ID, ListRow: r})
	}
	if err := s.deps.Lists.Create(ctx, list, rows); err != nil {
		return Preview{}, err
	}
	s.emailsSeen(ctx, ch, parsed.Rows...)
	return s.preview(ctx, a, list, view)
}

func (s *Service) emailsSeen(ctx context.Context, ch campaignaudience.Channel, rows ...campaign.ListRow) {
	if ch.Contact() != campaign.ContactEmail {
		return
	}
	addresses := make([]emailaddress.Address, 0, len(rows))
	for _, r := range rows {
		if address, err := emailaddress.Normalize(r.Contact); err == nil {
			addresses = append(addresses, address)
		}
	}
	if len(addresses) > 0 {
		s.deps.EmailsSeen.AddressesSeen(ctx, addresses)
	}
}

func listChannel(ch campaignaudience.Channel) error {
	if !ch.Valid() {
		return campaignaudience.ErrChannelInvalid
	}
	if ch == campaignaudience.ChannelCarrier {
		return campaignaudience.ErrSourceUnsupported
	}
	return nil
}

func (s *Service) Preview(ctx context.Context, a Actor, ch campaignaudience.Channel, listID string, view View) (Preview, error) {
	list, err := s.readable(ctx, a, ch, listID, "")
	if err != nil {
		return Preview{}, err
	}
	return s.preview(ctx, a, *list, view)
}

func (s *Service) preview(ctx context.Context, a Actor, list campaignaudience.List, view View) (Preview, error) {
	mapping, err := s.mappingFor(ctx, a, list, view)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{
		ID: list.ID, Channel: list.Channel, Contact: list.Channel.Contact(), FileName: list.FileName, Lines: list.Lines,
		Header: list.Layout.Header, VariableColumns: list.Layout.VariableColumns(), ExpiresAt: list.ExpiresAt,
		Columns: PreviewColumns{Contact: list.Layout.ContactLabel(), Name: list.Layout.NameLabel(), Variables: mapping.Variables, Metadata: mapping.MetadataColumns},
		Skipped: Skipped{Total: list.SkippedTotal(), ByReason: list.IssueCounts, Lines: list.Issues},
	}
	if p.Columns.Variables == nil {
		p.Columns.Variables = []campaign.VariableColumnMapping{}
	}
	if p.Columns.Metadata == nil {
		p.Columns.Metadata = []campaign.MetadataColumn{}
	}
	if p.Skipped.Lines == nil {
		p.Skipped.Lines = []campaign.ListIssue{}
	}
	gaps := campaignaudience.NewMetadataGaps(mapping.metadata)
	err = s.eachRowBatch(ctx, list, func(rows []campaignaudience.Row, existing map[string]string) error {
		p.Rows += len(rows)
		p.Existing += len(existing)
		for _, row := range rows {
			gaps.Add(row.Line, mapping.Metadata(row.ListRow))
		}
		return nil
	})
	if err != nil {
		return Preview{}, err
	}
	p.New = p.Rows - p.Existing
	p.MetadataMissing = gaps.Gaps()
	return p, nil
}

type mapped struct {
	campaign.ListMapping
	required int
	judge    Judge
	metadata campaignaudience.MetadataRule
}

func (s *Service) mappingFor(ctx context.Context, a Actor, list campaignaudience.List, view View) (mapped, error) {
	kind := list.Channel.Contact()
	if !view.hasContext() || kind != campaign.ContactPhone {
		required := 0
		if kind == campaign.ContactPhone {
			required = list.Layout.VariableColumns()
		}
		return mapped{ListMapping: campaign.MapList(list.Layout, kind, make([]campaign.BindingSource, required), nil), required: required}, nil
	}
	ch, err := s.channel(list.Channel)
	if err != nil {
		return mapped{}, err
	}
	prepared, err := ch.Prepare(ctx, a, view.Params)
	if err != nil {
		return mapped{}, err
	}
	bound, err := campaignaudience.Bind(prepared.Slots, view.Variables)
	if err != nil {
		return mapped{}, err
	}
	defs, err := s.definitions(a.WorkspaceID)
	if err != nil {
		return mapped{}, err
	}
	out := mapped{ListMapping: campaign.MapList(list.Layout, kind, campaignaudience.Fields(bound), defs), required: len(bound), metadata: prepared.Metadata}
	if prepared.Open != nil {
		if out.judge, err = prepared.Open(ctx, bound, campaignaudience.SourceList); err != nil {
			return mapped{}, err
		}
	}
	return out, nil
}

type RowsQuery struct {
	View
	Search      string
	MissingOnly bool
	Page        int
	Limit       int
}

type RowView struct {
	ID              string         `json:"id"`
	Line            int            `json:"line"`
	Contact         string         `json:"contact"`
	Name            string         `json:"name"`
	Variables       []string       `json:"variables"`
	Metadata        map[string]any `json:"metadata"`
	Missing         []int          `json:"missing"`
	MetadataMissing []string       `json:"metadataMissing"`
	Existing        bool           `json:"existing"`
}

type RowPage struct {
	Items []RowView
	Page  int
	Limit int
	Total int
}

func (s *Service) Rows(ctx context.Context, a Actor, ch campaignaudience.Channel, listID string, q RowsQuery) (RowPage, error) {
	if err := validPage(q.Page, q.Limit); err != nil {
		return RowPage{}, err
	}
	list, err := s.readable(ctx, a, ch, listID, "")
	if err != nil {
		return RowPage{}, err
	}
	m, err := s.mappingFor(ctx, a, *list, q.View)
	if err != nil {
		return RowPage{}, err
	}
	page := RowPage{Items: []RowView{}, Page: q.Page, Limit: q.Limit}
	if !q.MissingOnly {
		rows, total, err := s.deps.Lists.SearchRows(ctx, list.ID, campaignaudience.RowQuery{Search: q.Search, Offset: (q.Page - 1) * q.Limit, Limit: q.Limit})
		if err != nil {
			return RowPage{}, err
		}
		views, err := s.rowViews(ctx, *list, m, rows)
		if err != nil {
			return RowPage{}, err
		}
		page.Items, page.Total = views, total
		return page, nil
	}
	term := strings.ToLower(strings.TrimSpace(q.Search))
	found := newPager[RowView](q.Page, q.Limit)
	err = s.eachRowBatch(ctx, *list, func(rows []campaignaudience.Row, _ map[string]string) error {
		views, err := s.rowViews(ctx, *list, m, rows)
		if err != nil {
			return err
		}
		for _, v := range views {
			if v.incomplete() && matchesTerm(v, term) {
				found.add(v)
			}
		}
		return nil
	})
	if err != nil {
		return RowPage{}, err
	}
	page.Items, page.Total = found.items, found.total
	return page, nil
}

func (v RowView) incomplete() bool {
	return len(v.Missing) > 0 || len(v.MetadataMissing) > 0
}

func matchesTerm(v RowView, term string) bool {
	return term == "" || strings.Contains(strings.ToLower(v.Contact), term) || strings.Contains(strings.ToLower(v.Name), term)
}

func (s *Service) rowViews(ctx context.Context, list campaignaudience.List, m mapped, rows []campaignaudience.Row) ([]RowView, error) {
	existing, err := s.existing(ctx, list, rows)
	if err != nil {
		return nil, err
	}
	candidates := make([]RowCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, RowCandidate{Row: row, LeadID: existing[row.Contact], Values: m.Cells(row.ListRow), Metadata: m.Metadata(row.ListRow)})
	}
	values := make([][]string, len(candidates))
	for i, c := range candidates {
		values[i] = c.Values
	}
	if m.judge != nil {
		judged, err := m.judge.Rows(ctx, candidates)
		if err != nil {
			return nil, err
		}
		for i := range judged {
			if i < len(values) {
				values[i] = judged[i].Values
			}
		}
	}
	views := make([]RowView, 0, len(rows))
	for i, c := range candidates {
		v := RowView{
			ID: c.Row.ID, Line: c.Row.Line, Contact: c.Row.Contact, Name: c.Row.Name,
			Variables: values[i], Metadata: c.Metadata, Missing: campaign.MissingSlots(values[i]), Existing: c.LeadID != "",
		}
		if v.Variables == nil {
			v.Variables = []string{}
		}
		if v.Missing == nil {
			v.Missing = []int{}
		}
		if v.Metadata == nil {
			v.Metadata = map[string]any{}
		}
		v.MetadataMissing = []string{}
		if m.metadata != nil {
			v.MetadataMissing = append(v.MetadataMissing, m.metadata(c.Metadata)...)
		}
		views = append(views, v)
	}
	return views, nil
}

type RowInput struct {
	Contact   string
	Name      string
	Variables []string
	Metadata  map[string]any
}

func (in RowInput) row(kind campaign.ContactKind) (campaign.ListRow, error) {
	contact, issue := campaign.NormalizeListContact(in.Contact, kind)
	if issue != "" {
		return campaign.ListRow{}, campaignaudience.ErrRowContactInvalid
	}
	variables := make([]string, 0, len(in.Variables))
	for _, v := range in.Variables {
		variables = append(variables, strings.TrimSpace(v))
	}
	row := campaign.ListRow{Contact: contact, Name: strings.TrimSpace(in.Name), Variables: variables}
	if in.Metadata != nil {
		row.Metadata = in.Metadata
	}
	return row, nil
}

func (s *Service) editable(ctx context.Context, a Actor, ch campaignaudience.Channel, listID string) (*campaignaudience.List, error) {
	if err := listChannel(ch); err != nil {
		return nil, err
	}
	return s.readable(ctx, a, ch, listID, "")
}

func (s *Service) touch(ctx context.Context, list campaignaudience.List) error {
	list.Touch(s.now())
	return s.deps.Lists.Save(ctx, list)
}

func (s *Service) AddRow(ctx context.Context, a Actor, ch campaignaudience.Channel, listID string, in RowInput) (campaignaudience.Row, error) {
	list, err := s.editable(ctx, a, ch, listID)
	if err != nil {
		return campaignaudience.Row{}, err
	}
	parsed, err := in.row(ch.Contact())
	if err != nil {
		return campaignaudience.Row{}, err
	}
	count, err := s.deps.Lists.CountRows(ctx, list.ID)
	if err != nil {
		return campaignaudience.Row{}, err
	}
	if count >= campaign.MaxEntries {
		return campaignaudience.Row{}, campaign.ErrListTooLarge
	}
	row := campaignaudience.Row{ID: s.deps.NewID(), ListID: list.ID, ListRow: parsed}
	if err := s.deps.Lists.AddRow(ctx, row); err != nil {
		return campaignaudience.Row{}, err
	}
	s.emailsSeen(ctx, ch, parsed)
	return row, s.touch(ctx, *list)
}

func (s *Service) EditRow(ctx context.Context, a Actor, ch campaignaudience.Channel, listID, rowID string, in RowInput) (campaignaudience.Row, error) {
	list, err := s.editable(ctx, a, ch, listID)
	if err != nil {
		return campaignaudience.Row{}, err
	}
	parsed, err := in.row(ch.Contact())
	if err != nil {
		return campaignaudience.Row{}, err
	}
	current, err := s.findRow(ctx, list.ID, rowID)
	if err != nil {
		return campaignaudience.Row{}, err
	}
	parsed.Line, parsed.Cells = current.Line, current.Cells
	if parsed.Metadata == nil {
		parsed.Metadata = current.Metadata
	}
	row := campaignaudience.Row{ID: current.ID, ListID: list.ID, ListRow: parsed}
	if err := s.deps.Lists.UpdateRow(ctx, row); err != nil {
		return campaignaudience.Row{}, err
	}
	s.emailsSeen(ctx, ch, parsed)
	return row, s.touch(ctx, *list)
}

func (s *Service) findRow(ctx context.Context, listID, rowID string) (campaignaudience.Row, error) {
	return s.deps.Lists.Row(ctx, listID, strings.TrimSpace(rowID))
}

func (s *Service) RemoveRow(ctx context.Context, a Actor, ch campaignaudience.Channel, listID, rowID string) error {
	list, err := s.editable(ctx, a, ch, listID)
	if err != nil {
		return err
	}
	if err := s.deps.Lists.DeleteRow(ctx, list.ID, strings.TrimSpace(rowID)); err != nil {
		return err
	}
	return s.touch(ctx, *list)
}

func (s *Service) Attach(ctx context.Context, a Actor, listID, owner string) error {
	list, err := s.readable(ctx, a, campaignaudience.ChannelEmail, listID, "")
	if err != nil {
		return err
	}
	if list.AttachedTo == owner {
		return nil
	}
	if list.AttachedTo != "" {
		return campaignaudience.ErrListNotFound
	}
	list.AttachedTo = owner
	return s.touch(ctx, *list)
}

func (s *Service) Release(ctx context.Context, workspaceID, owner, listID string) error {
	list, err := s.deps.Lists.Get(ctx, workspaceID, strings.TrimSpace(listID))
	if errors.Is(err, campaignaudience.ErrListNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if list.OwnedBy(workspaceID, owner, list.Channel) != nil {
		return nil
	}
	return s.deps.Lists.Delete(ctx, workspaceID, list.ID)
}

func (s *Service) EnsureLeads(ctx context.Context, workspaceID, owner, listID string) error {
	list, err := s.deps.Lists.Get(ctx, workspaceID, strings.TrimSpace(listID))
	if err != nil {
		return err
	}
	if err := list.OwnedBy(workspaceID, owner, campaignaudience.ChannelEmail); err != nil {
		return err
	}
	return s.eachRowBatch(ctx, *list, func(rows []campaignaudience.Row, _ map[string]string) error {
		inputs := make([]lead.BulkEmailInput, 0, len(rows))
		for _, r := range rows {
			inputs = append(inputs, lead.BulkEmailInput{Source: lead.SourceImport, Email: r.Contact, Name: r.Name})
		}
		_, err := s.deps.EmailLeads.FindOrCreateManyByEmail(ctx, workspaceID, inputs)
		return err
	})
}

func (s *Service) Sweep(ctx context.Context) (int, error) {
	return s.deps.Lists.DeleteExpired(ctx, s.now(), sweepBatch)
}

func (s *Service) SweepExpired(ctx context.Context) error {
	_, err := s.Sweep(ctx)
	return err
}
