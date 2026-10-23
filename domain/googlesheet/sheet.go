package googlesheet

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	DefaultTimeZone = "America/Sao_Paulo"
	DefaultLocale   = "pt_BR"
	MaxTitleRunes   = 200
	MaxTabRunes     = 100
)

var localePattern = regexp.MustCompile(`^[a-z]{2}_[A-Z]{2}$`)

type Status string

const (
	StatusActive         Status = "active"
	StatusNeedsAttention Status = "needs_attention"
)

type AttentionReason string

const (
	ReasonHeaderMismatch    AttentionReason = "header_mismatch"
	ReasonAccessLost        AttentionReason = "access_lost"
	ReasonSpreadsheetGone   AttentionReason = "spreadsheet_missing"
	ReasonSheetFull         AttentionReason = "sheet_full"
	ReasonGoogleUnavailable AttentionReason = "google_unavailable"
	ReasonAccountChanged    AttentionReason = "account_changed"
)

type Sheet struct {
	ID            string
	WorkspaceID   string
	AccountID     string
	SpreadsheetID string
	TabID         int64
	TabName       string
	Title         string
	Description   string
	TimeZone      string
	Locale        string
	Columns       []Column
	Status        Status
	Reason        AttentionReason
	ReasonDetail  string
	Version       int
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastWrittenAt *time.Time
}

type NewSheetInput struct {
	WorkspaceID string
	AccountID   string
	Title       string
	TabName     string
	Description string
	TimeZone    string
	Locale      string
	CreatedBy   string
	Columns     []ColumnInput
}

func NewSheet(in NewSheetInput) (*Sheet, error) {
	return buildSheet(in, true)
}

func CheckDefinition(in NewSheetInput) error {
	_, err := buildSheet(in, false)
	return err
}

func buildSheet(in NewSheetInput, owned bool) (*Sheet, error) {
	s := &Sheet{
		WorkspaceID: strings.TrimSpace(in.WorkspaceID),
		AccountID:   strings.TrimSpace(in.AccountID),
		Title:       strings.TrimSpace(in.Title),
		TabName:     strings.TrimSpace(in.TabName),
		Description: strings.TrimSpace(in.Description),
		TimeZone:    strings.TrimSpace(in.TimeZone),
		Locale:      strings.TrimSpace(in.Locale),
		CreatedBy:   in.CreatedBy,
		Status:      StatusActive,
		Version:     1,
	}
	if s.TabName == "" {
		s.TabName = s.Title
	}
	if s.TimeZone == "" {
		s.TimeZone = DefaultTimeZone
	}
	if s.Locale == "" {
		s.Locale = DefaultLocale
	}
	if owned {
		if err := s.validateOwner(); err != nil {
			return nil, err
		}
	}
	if err := s.validateFrame(); err != nil {
		return nil, err
	}
	for _, input := range in.Columns {
		col, err := newColumn(input, s.keys())
		if err != nil {
			return nil, err
		}
		s.Columns = append(s.Columns, col)
	}
	if err := s.validateColumns(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Sheet) validateOwner() error {
	switch {
	case s.WorkspaceID == "":
		return invalidSheet("workspace is required")
	case s.AccountID == "":
		return invalidSheet("a connected google account is required")
	}
	return nil
}

func (s *Sheet) validateFrame() error {
	switch {
	case s.Title == "":
		return invalidSheet("title is required")
	case utf8.RuneCountInString(s.Title) > MaxTitleRunes:
		return invalidSheet("title is longer than %d characters", MaxTitleRunes)
	case utf8.RuneCountInString(s.TabName) > MaxTabRunes:
		return invalidSheet("tab name is longer than %d characters", MaxTabRunes)
	case strings.ContainsAny(s.TabName, "'[]*?/\\:"):
		return invalidSheet("tab name %q has a character google refuses", s.TabName)
	case !localePattern.MatchString(s.Locale):
		return invalidSheet("locale %q is not like pt_BR", s.Locale)
	}
	if _, err := time.LoadLocation(s.TimeZone); err != nil {
		return invalidSheet("unknown time zone %q", s.TimeZone)
	}
	return nil
}

func (s *Sheet) validateColumns() error {
	if len(s.Columns) == 0 {
		return invalidSheet("a sheet needs at least one column")
	}
	headers := map[string]bool{}
	inputs := 0
	for _, col := range s.Columns {
		folded := shared.FoldForMatch(col.Header)
		if headers[folded] {
			return invalidSheet("header %q is repeated", col.Header)
		}
		headers[folded] = true
		if col.TakesInput() {
			inputs++
		}
	}
	if inputs == 0 {
		return invalidSheet("a sheet needs at least one active column filled by agents or workflows")
	}
	return nil
}

func (s *Sheet) keys() []string {
	keys := make([]string, len(s.Columns))
	for i, col := range s.Columns {
		keys[i] = col.Key
	}
	return keys
}

func (s *Sheet) Column(key string) (Column, bool) {
	for _, col := range s.Columns {
		if col.Key == key {
			return col, true
		}
	}
	return Column{}, false
}

func (s *Sheet) InputColumns() []Column {
	var out []Column
	for _, col := range s.Columns {
		if col.TakesInput() {
			out = append(out, col)
		}
	}
	return out
}

func (s *Sheet) HeaderRow() []string {
	header := make([]string, 0, len(s.Columns)+1)
	header = append(header, RefHeader)
	for _, col := range s.Columns {
		header = append(header, col.Header)
	}
	return header
}

func (s *Sheet) Location() *time.Location {
	loc, err := time.LoadLocation(s.TimeZone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *Sheet) URL() string {
	return fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s/edit#gid=%d", s.SpreadsheetID, s.TabID)
}

func (s *Sheet) Writable() error {
	if s.Status != StatusActive {
		return fmt.Errorf("%w: %s", ErrSheetNotActive, s.Reason)
	}
	return nil
}

func (s *Sheet) NeedsAttention(reason AttentionReason, detail string) {
	s.Status, s.Reason, s.ReasonDetail = StatusNeedsAttention, reason, detail
}

func (s *Sheet) Activate() {
	s.Status, s.Reason, s.ReasonDetail = StatusActive, "", ""
}

type ColumnUpdate struct {
	Key         string
	Header      *string
	Instruction *string
	Required    *bool
	Archived    *bool
	Options     []string
}

type ColumnsEdit struct {
	Add    []ColumnInput
	Update []ColumnUpdate
}

func (e ColumnsEdit) Empty() bool { return len(e.Add) == 0 && len(e.Update) == 0 }

func (s *Sheet) EditColumns(edit ColumnsEdit) error {
	if edit.Empty() {
		return invalidSheet("the edit changes nothing")
	}
	columns := slices.Clone(s.Columns)
	for i := range columns {
		columns[i].Options = slices.Clone(columns[i].Options)
	}
	draft := *s
	draft.Columns = columns
	for _, update := range edit.Update {
		if err := draft.applyUpdate(update); err != nil {
			return err
		}
	}
	for _, input := range edit.Add {
		col, err := newColumn(input, draft.keys())
		if err != nil {
			return err
		}
		draft.Columns = append(draft.Columns, col)
	}
	if err := draft.validateColumns(); err != nil {
		return err
	}
	s.Columns = draft.Columns
	s.Version++
	return nil
}

func (s *Sheet) applyUpdate(update ColumnUpdate) error {
	index := -1
	for i, col := range s.Columns {
		if col.Key == update.Key {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("%w: %q", ErrUnknownColumn, update.Key)
	}
	col := s.Columns[index]
	if update.Header != nil {
		col.Header = strings.TrimSpace(*update.Header)
	}
	if update.Instruction != nil {
		col.Instruction = strings.TrimSpace(*update.Instruction)
	}
	if update.Required != nil {
		col.Required = *update.Required
	}
	if update.Archived != nil {
		col.Archived = *update.Archived
	}
	if update.Options != nil {
		if col.Type != TypeChoice {
			return invalidSheet("only single choice columns take options (%q)", col.Header)
		}
		next := trimmedOptions(update.Options)
		for _, existing := range col.Options {
			if !slices.Contains(next, existing) {
				return invalidSheet("option %q of %q may hold rows already; options can only be added", existing, col.Header)
			}
		}
		col.Options = next
	}
	if err := col.validate(); err != nil {
		return err
	}
	s.Columns[index] = col
	return nil
}

func (s *Sheet) Rename(title, description *string) error {
	draft := *s
	if title != nil {
		draft.Title = strings.TrimSpace(*title)
	}
	if description != nil {
		draft.Description = strings.TrimSpace(*description)
	}
	if err := draft.validateFrame(); err != nil {
		return err
	}
	s.Title, s.Description = draft.Title, draft.Description
	s.Version++
	return nil
}

func HeaderDifference(expected []string, rows [][]any) string {
	var actual []any
	if len(rows) > 0 {
		actual = rows[0]
	}
	for i, want := range expected {
		got := ""
		if i < len(actual) {
			got = strings.TrimSpace(fmt.Sprint(actual[i]))
		}
		if got != want {
			return fmt.Sprintf("coluna %s: esperado %q, encontrado %q", columnLetter(i), want, got)
		}
	}
	return ""
}

func columnLetter(index int) string {
	letters := ""
	for n := index + 1; n > 0; n = (n - 1) / 26 {
		letters = string(rune('A'+(n-1)%26)) + letters
	}
	return letters
}

const DashboardPath = "/dashboard/integrations/google-sheets"

func (s *Sheet) DashboardLink() string { return DashboardPath + "/" + s.ID }

func (s *Sheet) CheckMapping(mapping map[string]string) error {
	for key := range mapping {
		col, ok := s.Column(key)
		switch {
		case !ok:
			return &ValueError{Column: key, Header: key, Reason: ErrUnknownColumn}
		case col.Archived:
			return &ValueError{Column: key, Header: col.Header, Reason: ErrColumnArchived}
		case col.Source.IsSystem():
			return &ValueError{Column: key, Header: col.Header, Reason: ErrSystemColumn}
		}
	}
	for _, col := range s.InputColumns() {
		if col.Required && strings.TrimSpace(mapping[col.Key]) == "" {
			return &ValueError{Column: col.Key, Header: col.Header, Reason: ErrRequiredColumnMissing}
		}
	}
	return nil
}
