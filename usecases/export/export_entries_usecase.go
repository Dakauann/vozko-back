package export_usecase

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"vozko/domain/analysis"
	"vozko/domain/export"
	shared_domain "vozko/domain/shared"
	"vozko/domain/stage"
)

const (
	// maxExportRows is the ceiling on a single export. It is a refusal rather
	// than a truncation: a file that silently stops at the cap looks complete
	// and gets acted on as if it were. The operator narrows the period instead.
	maxExportRows = 50_000

	// enrichBatchSize is how many rows are held in memory at once. Analyses and
	// stages are looked up per batch, so memory is O(batch) no matter how large
	// the scope is, and each lookup stays a bounded indexed IN over ids we
	// already have — never an N+1 and never a 50k-element IN clause.
	enrichBatchSize = 500

	// maxMetaColumns bounds the header. Metadata keys come from operator
	// uploads, so a workspace that shipped a per-row unique key would otherwise
	// widen the file until it stopped opening anywhere.
	maxMetaColumns = 200
)

// AnalysisLookup and StageLookup are the two reads this usecase makes beyond
// the channel's own rows. They are declared here, narrowed to the single method
// each, rather than taking the full repositories: the concrete repositories
// satisfy them implicitly, so nothing changes at the wiring, and a test does not
// have to stub forty methods it never calls to exercise a CSV.
type AnalysisLookup interface {
	FindLatestByEntries(entryIDs []string, entryType shared_domain.EntryType) (map[string]*analysis.Analysis, error)
}

type StageLookup interface {
	GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*stage.EntryStage, error)
}

type exportEntriesUseCase struct {
	analysisRepo AnalysisLookup
	stageRepo    StageLookup
	// listers supply channel-neutral rows for every channel, WhatsApp included.
	// Keyed by entry type so registering one never displaces another.
	listers map[export.EntryType]export.ChannelEntryLister
}

// SetChannelEntryLister registers a channel's export source.
//
// Without one, that channel's conversations cannot be exported at all, the old
// behaviour for everything except WhatsApp, which returned "unsupported entry
// type" and gave an operator no way to get their data out.
func (uc *exportEntriesUseCase) SetChannelEntryLister(entryType export.EntryType, lister export.ChannelEntryLister) {
	if uc == nil || lister == nil || entryType == "" {
		return
	}
	if uc.listers == nil {
		uc.listers = make(map[export.EntryType]export.ChannelEntryLister, 4)
	}
	uc.listers[entryType] = lister
}

func NewExportEntriesUseCase(
	analysisRepo AnalysisLookup,
	stageRepo StageLookup,
) export.ExportEntriesUseCase {
	return &exportEntriesUseCase{
		analysisRepo: analysisRepo,
		stageRepo:    stageRepo,
	}
}

// Export walks the scope twice.
//
// The first walk measures: how many rows, how many template variables, which
// metadata keys. That is what the CSV header is made of, and a header cannot be
// written after the rows it labels — so a single-pass export would have to hold
// every row in memory to learn its own shape. The second walk streams rows out
// as they arrive, holding one batch at a time.
//
// The cost is reading the scope twice; the benefit is that a workspace-wide
// export of hundreds of thousands of entries uses the same memory as one of
// fifty, and that the row cap is enforced before a single byte is written.
func (uc *exportEntriesUseCase) Export(ctx context.Context, filter export.ExportFilter, w io.Writer) (int, error) {
	if strings.TrimSpace(filter.Scope.WorkspaceID) == "" {
		return 0, fmt.Errorf("workspace id is required")
	}

	lister, ok := uc.listers[filter.EntryType]
	if !ok {
		return 0, fmt.Errorf("unsupported entry type: %s", filter.EntryType)
	}

	shape, err := uc.measure(ctx, lister, filter)
	if err != nil {
		return 0, err
	}
	// Nothing matched, so nothing is written — not even a header. The caller
	// can still answer with a status code of its choosing.
	if shape.rows == 0 {
		return 0, nil
	}

	return uc.stream(ctx, lister, filter, shape, w)
}

// csvShape is everything about the file that has to be known before its first
// line: which optional column groups exist and how wide they are.
type csvShape struct {
	rows    int
	maxVars int
	// metaKeys is sorted, so two exports of the same data produce byte-identical
	// column order.
	metaKeys []string

	includeVariables bool
	includeCampaign  bool
}

func (uc *exportEntriesUseCase) measure(
	ctx context.Context,
	lister export.ChannelEntryLister,
	filter export.ExportFilter,
) (csvShape, error) {
	shape := csvShape{
		// Variables are template-positional, so variable_1 means one thing in
		// one campaign and something else in the next. They are still carried
		// across campaigns because the campaign column makes them readable, but
		// only WhatsApp has them at all.
		includeVariables: filter.EntryType == export.EntryTypeWhatsApp,
		includeCampaign:  filter.Scope.SpansContainers(),
	}

	metaKeys := make(map[string]struct{})
	truncatedMeta := false

	err := lister.ListForExport(ctx, filter.Scope, func(e export.ChannelEntry) error {
		if !matchesEntryFilter(filter, e) {
			return nil
		}

		shape.rows++
		if shape.rows > maxExportRows {
			return export.ErrTooManyRows
		}

		if shape.includeVariables && len(e.Variables) > shape.maxVars {
			shape.maxVars = len(e.Variables)
		}
		if !truncatedMeta {
			for k := range e.Metadata {
				if _, seen := metaKeys[k]; seen {
					continue
				}
				if len(metaKeys) >= maxMetaColumns {
					truncatedMeta = true
					break
				}
				metaKeys[k] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return csvShape{}, err
	}

	shape.metaKeys = make([]string, 0, len(metaKeys))
	for k := range metaKeys {
		shape.metaKeys = append(shape.metaKeys, k)
	}
	sort.Strings(shape.metaKeys)

	return shape, nil
}

func (uc *exportEntriesUseCase) stream(
	ctx context.Context,
	lister export.ChannelEntryLister,
	filter export.ExportFilter,
	shape csvShape,
	w io.Writer,
) (int, error) {
	sink := &csvSink{writer: csv.NewWriter(w), shape: shape, entryType: filter.EntryType}
	batch := make([]export.ChannelEntry, 0, enrichBatchSize)

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := uc.writeBatch(filter, batch, sink); err != nil {
			return err
		}
		batch = batch[:0]
		// Hand the bytes to the transport now rather than at the end, so a long
		// export arrives as a download in progress instead of a stalled request.
		sink.writer.Flush()
		return sink.writer.Error()
	}

	err := lister.ListForExport(ctx, filter.Scope, func(e export.ChannelEntry) error {
		if !matchesEntryFilter(filter, e) {
			return nil
		}
		batch = append(batch, e)
		if len(batch) < enrichBatchSize {
			return nil
		}
		return flushBatch()
	})
	if err != nil {
		return sink.count, err
	}
	if err := flushBatch(); err != nil {
		return sink.count, err
	}

	sink.writer.Flush()
	if err := sink.writer.Error(); err != nil {
		return sink.count, err
	}
	return sink.count, nil
}

// writeBatch enriches one window of rows and writes it.
//
// Analyses and stages key on (entry_id, entry_type), so they are read exactly
// the same way for every channel — no per-channel branch, and one lookup per
// batch rather than one per row.
func (uc *exportEntriesUseCase) writeBatch(
	filter export.ExportFilter,
	batch []export.ChannelEntry,
	sink *csvSink,
) error {
	entryIDs := make([]string, len(batch))
	for i, e := range batch {
		entryIDs[i] = e.EntryID
	}

	analysisMap, err := uc.analysisRepo.FindLatestByEntries(entryIDs, shared_domain.EntryType(filter.EntryType))
	if err != nil {
		return fmt.Errorf("load analyses: %w", err)
	}
	stageMap, err := uc.stageRepo.GetBatchEntryStages(entryIDs, string(filter.EntryType), filter.Scope.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load tags: %w", err)
	}

	for _, e := range batch {
		a := analysisMap[e.EntryID]
		entryStage := stageMap[e.EntryID]

		row := export.ExportRow{
			Number:       e.Number,
			Name:         e.Name,
			Age:          e.Age,
			CampaignName: e.ContainerName,
			Status:       e.Status,
			CreatedAt:    e.CreatedAt,
			UpdatedAt:    e.UpdatedAt,
			Variables:    e.Variables,
			Metadata:     e.Metadata,
		}
		if entryStage != nil {
			row.StageName = entryStage.StageName
		}
		populateAnalysisFields(&row, a)

		if !matchesEnrichedFilter(filter, a, entryStage) {
			continue
		}
		if err := sink.write(row); err != nil {
			return err
		}
	}
	return nil
}

// csvSink writes the header lazily, immediately before the first data row.
//
// That is what lets Export write nothing at all when the filter excludes
// everything: a file containing only a header is not "no results", it is an
// empty spreadsheet an operator has to open to find that out.
type csvSink struct {
	writer    *csv.Writer
	shape     csvShape
	entryType export.EntryType
	started   bool
	count     int
}

func (s *csvSink) header() []string {
	header := make([]string, 0, 16+s.shape.maxVars+len(s.shape.metaKeys))
	if s.shape.includeCampaign {
		header = append(header, "campaign")
	}
	header = append(header, "number", "name", "age", "status", "tag", "created_at", "updated_at")

	if s.shape.includeVariables {
		for i := 1; i <= s.shape.maxVars; i++ {
			header = append(header, fmt.Sprintf("variable_%d", i))
		}
	}
	for _, k := range s.shape.metaKeys {
		header = append(header, "meta_"+k)
	}

	return append(header,
		"analysis_interest",
		"analysis_disposition",
		"analysis_sentiment",
		"analysis_qualification",
		"analysis_next_action",
		"analysis_attendance_quality",
		"analysis_summary",
		"analysis_product_interest",
	)
}

func (s *csvSink) write(row export.ExportRow) error {
	if !s.started {
		if err := s.writer.Write(s.header()); err != nil {
			return fmt.Errorf("write header: %w", err)
		}
		s.started = true
	}

	record := make([]string, 0, 16+s.shape.maxVars+len(s.shape.metaKeys))
	if s.shape.includeCampaign {
		record = append(record, safeCSVText(row.CampaignName))
	}
	record = append(record,
		formatNumber(row.Number),
		safeCSVText(row.Name),
		formatOptionalInt(row.Age),
		row.Status,
		safeCSVText(row.StageName),
		row.CreatedAt,
		row.UpdatedAt,
	)

	if s.shape.includeVariables {
		for i := 0; i < s.shape.maxVars; i++ {
			if i < len(row.Variables) {
				record = append(record, safeCSVText(row.Variables[i]))
			} else {
				record = append(record, "")
			}
		}
	}

	for _, k := range s.shape.metaKeys {
		val := ""
		if row.Metadata != nil {
			if v, ok := row.Metadata[k]; ok && v != nil {
				val = formatMetaValue(v)
			}
		}
		record = append(record, val)
	}

	record = append(record,
		row.AnalysisInterest,
		row.AnalysisDisposition,
		row.AnalysisSentiment,
		row.AnalysisQualification,
		row.AnalysisNextAction,
		formatOptionalInt(row.AnalysisAttendanceQuality),
		safeCSVText(row.AnalysisSummary),
		safeCSVText(row.AnalysisProductInterest),
	)

	if err := s.writer.Write(record); err != nil {
		return fmt.Errorf("write row: %w", err)
	}
	s.count++
	return nil
}

func populateAnalysisFields(row *export.ExportRow, a *analysis.Analysis) {
	if a == nil {
		return
	}
	row.AnalysisInterest = string(a.Interest)
	row.AnalysisDisposition = string(a.Disposition)
	row.AnalysisSentiment = string(a.Sentiment)
	row.AnalysisQualification = string(a.Qualification)
	row.AnalysisNextAction = string(a.NextAction)
	aq := a.AttendanceQuality
	row.AnalysisAttendanceQuality = &aq
	row.AnalysisSummary = a.Summary
	if a.ProductInterest != nil {
		row.AnalysisProductInterest = *a.ProductInterest
	}
}

// matchesEntryFilter holds the predicates answerable from the channel row
// alone, so they can be applied on both walks and keep the measured row count
// equal to the written one.
//
// Status is re-checked here even though every lister filters it in SQL: the
// port cannot enforce that, and an export that quietly includes statuses the
// operator excluded is worse than one that costs a string comparison per row.
func matchesEntryFilter(f export.ExportFilter, e export.ChannelEntry) bool {
	if len(f.Scope.Statuses) > 0 && !containsFold(f.Scope.Statuses, e.Status) {
		return false
	}
	if f.Number != "" && !strings.Contains(e.Number, f.Number) {
		return false
	}
	return true
}

// matchesEnrichedFilter holds the predicates that need data the channel query
// does not carry. They run on the second walk only, which is why the header can
// name a column that every surviving row leaves blank.
func matchesEnrichedFilter(f export.ExportFilter, a *analysis.Analysis, t *stage.EntryStage) bool {
	if f.StageID != "" {
		if t == nil || t.StageID != f.StageID {
			return false
		}
	}

	if f.HasAnalysis != nil {
		hasA := a != nil
		if *f.HasAnalysis != hasA {
			return false
		}
	}
	if f.Interest != "" && (a == nil || !strings.EqualFold(string(a.Interest), f.Interest)) {
		return false
	}
	if f.Disposition != "" && (a == nil || !strings.EqualFold(string(a.Disposition), f.Disposition)) {
		return false
	}
	if f.Sentiment != "" && (a == nil || !strings.EqualFold(string(a.Sentiment), f.Sentiment)) {
		return false
	}
	if f.Qualification != "" && (a == nil || !strings.EqualFold(string(a.Qualification), f.Qualification)) {
		return false
	}
	if f.NextAction != "" && (a == nil || !strings.EqualFold(string(a.NextAction), f.NextAction)) {
		return false
	}
	if f.AttendanceQualityMin != nil && (a == nil || a.AttendanceQuality < *f.AttendanceQualityMin) {
		return false
	}
	if f.AttendanceQualityMax != nil && (a == nil || a.AttendanceQuality > *f.AttendanceQualityMax) {
		return false
	}

	return true
}

func containsFold(haystack []string, needle string) bool {
	for _, v := range haystack {
		if strings.EqualFold(v, needle) {
			return true
		}
	}
	return false
}

func formatNumber(number string) string {
	if number == "" {
		return ""
	}

	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, number)
	return cleaned
}

func formatOptionalInt(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func formatMetaValue(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return safeCSVText(fmt.Sprintf("%v", v))
		}
		return safeCSVText(string(b))
	default:
		return safeCSVText(fmt.Sprintf("%v", v))
	}
}

// safeCSVText prepares a free-text cell for a spreadsheet.
//
// Newlines are flattened so one record stays one line. The leading-quote is
// formula injection defence: Excel and Sheets execute a cell beginning with
// =, +, -, @, tab or CR, and every string that reaches here — contact names,
// campaign names, uploaded metadata, AI summaries — is written by someone
// outside this system. A crafted lead name would otherwise run as a formula on
// the machine of whoever opens the export.
//
// The number column is deliberately not passed through this: formatNumber has
// already reduced it to digits and a leading +, which cannot carry a payload,
// and an apostrophe in the column operators paste into dialers would break the
// file's main job.
func safeCSVText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t':
		return "'" + s
	}
	return s
}
