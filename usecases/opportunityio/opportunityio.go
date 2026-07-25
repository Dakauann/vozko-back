// Package opportunityio implements CSV import and export for sales
// opportunities. It is a thin transport layer over the existing opportunity
// usecase: it never writes opportunities directly, it maps CSV rows to
// opportunity_usecase.CreateInput and delegates creation (and dry-run
// validation) to that usecase so the custom-field and entity rules stay in one
// place.
//
// MONEY GUARDRAIL: the "value" column is the CUSTOMER's own sales figure in the
// major unit of the row Currency (e.g. reais). It is parsed to ValueCents (minor
// units) and is never Vozko wallet money.
package opportunityio

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
	opportunity_usecase "vozko/usecases/opportunity"
)

const (
	// objectType is the custom-field object discriminator for opportunities.
	objectType = "opportunity"
	// customFieldPrefix marks a CSV column that carries a custom field value; the
	// remainder of the column name is the custom field key.
	customFieldPrefix = "custom_field:"
	// multiSelectDelim separates the members of a multiselect custom field inside
	// a single CSV cell.
	multiSelectDelim = "|"
	// MaxImportRows caps a single import so a huge upload cannot exhaust memory or
	// hammer the database. Rows beyond the cap are NOT processed and the report
	// flags Truncated so the caller knows the file was not fully consumed.
	MaxImportRows = 5000
)

var (
	// ErrPipelineRequired is returned by Export when no pipeline is supplied.
	ErrPipelineRequired = errors.New("opportunityio: pipelineId is required")
	// ErrInvalidValue is returned when the "value" column is not a parseable
	// monetary amount.
	ErrInvalidValue = errors.New("opportunityio: invalid monetary value")
	// ErrInvalidDate is returned when the "close_date" column is not a parseable
	// date.
	ErrInvalidDate = errors.New("opportunityio: invalid date")
)

// OpportunityService is the slice of the opportunity usecase this package
// reuses. *opportunity_usecase.Service satisfies it, so creation and validation
// logic is never duplicated here.
type OpportunityService interface {
	ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*opportunity.Opportunity, error)
	Create(workspaceID string, in opportunity_usecase.CreateInput) (*opportunity.Opportunity, error)
	ValidateCreate(workspaceID string, in opportunity_usecase.CreateInput) error
}

// FieldLister lists a workspace's custom field definitions.
// customfield.Repository satisfies it. It is used to order the export columns
// and to coerce CSV strings to the right typed value on import.
type FieldLister interface {
	ListByObject(workspaceID, objectType string) ([]*customfield.Definition, error)
}

// Service performs opportunity CSV import/export.
type Service struct {
	opps   OpportunityService
	fields FieldLister
}

// NewService wires the CSV usecase from the opportunity usecase and the custom
// field lister. fields may be nil in contexts without custom fields.
func NewService(opps OpportunityService, fields FieldLister) *Service {
	return &Service{opps: opps, fields: fields}
}

// ---- Export ----

// exportFixedColumns are the non-custom-field columns, in output order. "value"
// is the deal amount in MAJOR units (e.g. reais), not cents.
var exportFixedColumns = []string{
	"id", "title", "value", "currency", "status",
	"stage_id", "owner_id", "lead_id", "source", "close_date", "created_at",
}

// Export streams the workspace's opportunities for one pipeline as CSV to w and
// returns the number of rows written (excluding the header). Custom fields are
// appended as one column per key, named "custom_field:<key>".
func (s *Service) Export(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string, w io.Writer) (int, error) {
	if strings.TrimSpace(pipelineID) == "" {
		return 0, ErrPipelineRequired
	}
	// Department-scoped: a restricted user must not export deals outside their scope.
	opps, err := s.opps.ListByPipelineScoped(workspaceID, pipelineID, departmentIDs, restrict, assigneeOverrideUserID)
	if err != nil {
		return 0, err
	}
	keys := s.customFieldKeys(workspaceID, opps)

	writer := csv.NewWriter(w)

	header := make([]string, 0, len(exportFixedColumns)+len(keys))
	header = append(header, exportFixedColumns...)
	for _, k := range keys {
		header = append(header, customFieldPrefix+k)
	}
	if err := writer.Write(header); err != nil {
		return 0, err
	}

	for _, o := range opps {
		record := []string{
			o.ID,
			o.Title,
			formatCentsToMajor(o.ValueCents),
			o.Currency,
			string(o.Status),
			o.StageID,
			o.OwnerID,
			o.LeadID,
			o.Source,
			formatTimePtr(o.CloseDate),
			o.CreatedAt.UTC().Format(time.RFC3339),
		}
		for _, k := range keys {
			record = append(record, formatCustomValue(o.CustomFields[k]))
		}
		if err := writer.Write(record); err != nil {
			return 0, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return 0, err
	}
	return len(opps), nil
}

// customFieldKeys returns the ordered set of custom field keys to export:
// defined fields first (ordered by Position then key), then any extra keys that
// exist in stored data but have no definition (sorted), so export never
// silently drops a stored value.
func (s *Service) customFieldKeys(workspaceID string, opps []*opportunity.Opportunity) []string {
	seen := map[string]struct{}{}
	var keys []string

	if s.fields != nil {
		if defs, err := s.fields.ListByObject(workspaceID, objectType); err == nil {
			sort.SliceStable(defs, func(i, j int) bool {
				if defs[i].Position != defs[j].Position {
					return defs[i].Position < defs[j].Position
				}
				return defs[i].Key < defs[j].Key
			})
			for _, d := range defs {
				if _, ok := seen[d.Key]; ok {
					continue
				}
				seen[d.Key] = struct{}{}
				keys = append(keys, d.Key)
			}
		}
	}

	var extra []string
	for _, o := range opps {
		for k := range o.CustomFields {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

// ---- Import ----

// ImportOptions tunes an import run.
type ImportOptions struct {
	// DefaultPipelineID backs rows that omit a pipeline_id column (e.g. when
	// re-importing an export, which does not emit that column). May be empty.
	DefaultPipelineID string
	// DryRun validates every row without creating anything.
	DryRun bool
}

// ImportError describes one rejected row. Row is 1-based and counts the header
// as row 1, so the first data row is row 2, matching what a spreadsheet shows.
type ImportError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ImportReport is the JSON result of an import. Every rejected row appears in
// Errors: rows are never silently dropped.
type ImportReport struct {
	Total   int           `json:"total"`
	Created int           `json:"created"`
	Skipped int           `json:"skipped"`
	Errors  []ImportError `json:"errors"`
	// Truncated is true when the file held more than MaxImportRows data rows; the
	// rows beyond the cap were NOT processed.
	Truncated bool `json:"truncated,omitempty"`
}

// Import parses CSV from r, maps each row to an opportunity and (unless DryRun)
// creates it through the opportunity usecase. On DryRun it validates each row
// without persisting; Created then counts rows that WOULD be created.
func (s *Service) Import(workspaceID string, r io.Reader, opts ImportOptions) (*ImportReport, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate ragged rows; each row is validated
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err == io.EOF {
		return &ImportReport{Errors: []ImportError{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	colIndex := indexHeader(header)

	// Load custom field definitions once so values coerce to the right type and
	// unknown keys can still be surfaced (via the usecase) rather than dropped.
	defsByKey := map[string]*customfield.Definition{}
	if s.fields != nil {
		if defs, e := s.fields.ListByObject(workspaceID, objectType); e == nil {
			for _, d := range defs {
				defsByKey[d.Key] = d
			}
		}
	}

	report := &ImportReport{Errors: []ImportError{}}
	rowNum := 1 // header is row 1
	for {
		record, e := reader.Read()
		if e == io.EOF {
			break
		}
		rowNum++

		// Cap guard: stop before processing beyond the limit and flag truncation
		// rather than silently ignoring the remainder.
		if report.Total >= MaxImportRows {
			report.Truncated = true
			break
		}

		if e != nil {
			report.Total++
			report.Skipped++
			report.Errors = append(report.Errors, ImportError{Row: rowNum, Message: "malformed CSV row: " + e.Error()})
			continue
		}
		if isBlankRecord(record) {
			continue // ignore stray blank lines without counting them
		}

		report.Total++
		in, buildErr := buildInput(colIndex, record, defsByKey, opts.DefaultPipelineID)
		if buildErr != nil {
			report.Skipped++
			report.Errors = append(report.Errors, ImportError{Row: rowNum, Message: buildErr.Error()})
			continue
		}

		if opts.DryRun {
			if verr := s.opps.ValidateCreate(workspaceID, in); verr != nil {
				report.Skipped++
				report.Errors = append(report.Errors, ImportError{Row: rowNum, Message: verr.Error()})
				continue
			}
			report.Created++
			continue
		}

		if _, cerr := s.opps.Create(workspaceID, in); cerr != nil {
			report.Skipped++
			report.Errors = append(report.Errors, ImportError{Row: rowNum, Message: cerr.Error()})
			continue
		}
		report.Created++
	}

	return report, nil
}

// buildInput maps one CSV record to a CreateInput. It parses the monetary value
// and close date and coerces custom field cells to typed values; it returns an
// error (never a partial silent drop) when a cell cannot be parsed.
func buildInput(idx map[string]int, record []string, defsByKey map[string]*customfield.Definition, defaultPipelineID string) (opportunity_usecase.CreateInput, error) {
	get := func(col string) string {
		i, ok := idx[col]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	in := opportunity_usecase.CreateInput{
		LeadID:       get("lead_id"),
		PipelineID:   get("pipeline_id"),
		StageID:      get("stage_id"),
		OwnerID:      get("owner_id"),
		Title:        get("title"),
		Currency:     get("currency"),
		Source:       get("source"),
		Status:       opportunity.Status(strings.ToLower(get("status"))),
		LostReasonID: get("lost_reason_id"),
	}
	if in.PipelineID == "" {
		in.PipelineID = defaultPipelineID
	}

	if v := get("value"); v != "" {
		cents, err := parseMajorToCents(v)
		if err != nil {
			return in, fmt.Errorf("value %q: %w", v, err)
		}
		in.ValueCents = cents
	}

	if v := get("close_date"); v != "" {
		t, err := parseDate(v)
		if err != nil {
			return in, fmt.Errorf("close_date %q: %w", v, err)
		}
		in.CloseDate = &t
	}

	custom := map[string]any{}
	for col, i := range idx {
		if !strings.HasPrefix(col, customFieldPrefix) || i >= len(record) {
			continue
		}
		raw := strings.TrimSpace(record[i])
		if raw == "" {
			continue
		}
		key := strings.TrimPrefix(col, customFieldPrefix)
		val, err := coerceCustomValue(raw, defsByKey[key])
		if err != nil {
			return in, fmt.Errorf("custom field %q: %w", key, err)
		}
		custom[key] = val
	}
	if len(custom) > 0 {
		in.CustomFields = custom
	}
	return in, nil
}

// coerceCustomValue turns a raw CSV cell into the typed value the custom-field
// validator expects. When def is nil (unknown key) the raw string is passed
// through so the usecase rejects it as an unknown field rather than dropping it.
func coerceCustomValue(raw string, def *customfield.Definition) (any, error) {
	if def == nil {
		return raw, nil
	}
	switch def.Type {
	case customfield.TypeNumber:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("not a number: %q", raw)
		}
		return f, nil
	case customfield.TypeBoolean:
		b, err := strconv.ParseBool(strings.ToLower(raw))
		if err != nil {
			return nil, fmt.Errorf("not a boolean: %q", raw)
		}
		return b, nil
	case customfield.TypeMultiSelect:
		parts := strings.Split(raw, multiSelectDelim)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	default: // text, date, select
		return raw, nil
	}
}

// ---- formatting / parsing helpers ----

// formatCentsToMajor renders minor units as a fixed 2-decimal major-unit string
// (e.g. 490000 -> "4900.00").
func formatCentsToMajor(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	out := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	if neg {
		out = "-" + out
	}
	return out
}

// parseMajorToCents parses a major-unit string ("4900.00", "1.234,56",
// "1,234.56") into minor units, rounding half-up to two decimals.
func parseMajorToCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	neg := false
	switch s[0] {
	case '-':
		neg, s = true, s[1:]
	case '+':
		s = s[1:]
	}

	// Normalise thousands/decimal separators to a single '.' decimal.
	hasComma := strings.Contains(s, ",")
	hasDot := strings.Contains(s, ".")
	switch {
	case hasComma && hasDot:
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			// "1.234,56" -> dot = thousands, comma = decimal
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// "1,234.56" -> comma = thousands
			s = strings.ReplaceAll(s, ",", "")
		}
	case hasComma:
		s = strings.ReplaceAll(s, ",", ".")
	}

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if !isDigits(intPart) || !isDigits(fracPart) {
		return 0, ErrInvalidValue
	}

	roundUp := false
	switch {
	case len(fracPart) < 2:
		fracPart += strings.Repeat("0", 2-len(fracPart))
	case len(fracPart) > 2:
		if fracPart[2] >= '5' {
			roundUp = true
		}
		fracPart = fracPart[:2]
	}

	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, ErrInvalidValue
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, ErrInvalidValue
	}
	cents := whole*100 + frac
	if roundUp {
		cents++
	}
	if neg {
		cents = -cents
	}
	return cents, nil
}

// parseDate accepts RFC3339, "2006-01-02" and "2006-01-02 15:04:05". Ambiguous
// slash formats are rejected on purpose so an import cannot silently swap day
// and month.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalidDate
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatCustomValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case []string:
		return strings.Join(val, multiSelectDelim)
	case []any:
		parts := make([]string, 0, len(val))
		for _, it := range val {
			parts = append(parts, fmt.Sprintf("%v", it))
		}
		return strings.Join(parts, multiSelectDelim)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// indexHeader maps normalised (trimmed, lowercased, BOM-stripped) column names
// to their index. The first name wins on a duplicate. Custom field keys are
// already lowercase (customfield.Normalize), so lowercasing is consistent.
func indexHeader(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		name := strings.TrimPrefix(strings.TrimSpace(h), "\uFEFF")
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, exists := idx[name]; !exists {
			idx[name] = i
		}
	}
	return idx
}

func isBlankRecord(record []string) bool {
	for _, f := range record {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
