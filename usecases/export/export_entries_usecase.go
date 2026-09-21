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

	ca "vozko/domain/audience"
	"vozko/domain/export"
	shared_domain "vozko/domain/shared"
	"vozko/domain/stage"
)

const (
	maxExportRows = 50_000

	enrichBatchSize = 500

	maxMetaColumns = 200
)

type AnalysisLookup interface {
	LatestByEntries(ctx context.Context, workspaceID string, source ca.Source, entryIDs []string) (map[string]*ca.Analysis, error)
}

type StageLookup interface {
	GetBatchEntryStages(entryIDs []string, entryType, workspaceID string) (map[string]*stage.EntryStage, error)
}

type exportEntriesUseCase struct {
	analysisRepo AnalysisLookup
	stageRepo    StageLookup
	listers      map[export.EntryType]export.ChannelEntryLister
}

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
	if shape.rows == 0 {
		return 0, nil
	}

	return uc.stream(ctx, lister, filter, shape, w)
}

type csvShape struct {
	rows     int
	maxVars  int
	metaKeys []string

	includeVariables bool
	includeCampaign  bool
	includeFailure   bool
}

func (uc *exportEntriesUseCase) measure(
	ctx context.Context,
	lister export.ChannelEntryLister,
	filter export.ExportFilter,
) (csvShape, error) {
	shape := csvShape{
		includeVariables: filter.EntryType == export.EntryTypeWhatsApp,
		includeCampaign:  filter.Scope.SpansContainers(),
		includeFailure:   filter.EntryType.HasSendStatus(),
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

func (uc *exportEntriesUseCase) writeBatch(
	filter export.ExportFilter,
	batch []export.ChannelEntry,
	sink *csvSink,
) error {
	entryIDs := make([]string, 0, len(batch))
	for _, e := range batch {
		if e.EntryID != "" {
			entryIDs = append(entryIDs, e.EntryID)
		}
	}

	var (
		analysisMap map[string]*ca.Analysis
		stageMap    map[string]*stage.EntryStage
	)
	if len(entryIDs) > 0 {
		var err error
		analysisMap, err = uc.analysisRepo.LatestByEntries(
			context.Background(), filter.Scope.WorkspaceID,
			ca.SourceOf(shared_domain.EntryType(filter.EntryType)), entryIDs)
		if err != nil {
			return fmt.Errorf("load analyses: %w", err)
		}
		stageMap, err = uc.stageRepo.GetBatchEntryStages(entryIDs, string(filter.EntryType), filter.Scope.WorkspaceID)
		if err != nil {
			return fmt.Errorf("load tags: %w", err)
		}
	}

	for _, e := range batch {
		a := analysisMap[e.EntryID]
		entryStage := stageMap[e.EntryID]

		row := export.ExportRow{
			Number:        e.Number,
			Name:          e.Name,
			Age:           e.Age,
			CampaignName:  e.ContainerName,
			Status:        e.Status,
			CreatedAt:     e.CreatedAt,
			UpdatedAt:     e.UpdatedAt,
			FailureCode:   e.FailureCode,
			FailureReason: e.FailureReason,
			Variables:     e.Variables,
			Metadata:      e.Metadata,
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
	header = append(header, "number", "name", "age", "status")
	if s.shape.includeFailure {
		header = append(header, "failure_code", "failure_reason")
	}
	header = append(header, "tag", "created_at", "updated_at")

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
	)
	if s.shape.includeFailure {
		record = append(record, formatErrorCode(row.FailureCode), safeCSVText(row.FailureReason))
	}
	record = append(record,
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

func populateAnalysisFields(row *export.ExportRow, a *ca.Analysis) {
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
	row.AnalysisProductInterest = a.ProductInterest
}

func matchesEntryFilter(f export.ExportFilter, e export.ChannelEntry) bool {
	if len(f.Scope.Statuses) > 0 && !containsFold(f.Scope.Statuses, e.Status) {
		return false
	}
	if f.Number != "" && !strings.Contains(e.Number, f.Number) {
		return false
	}
	return true
}

func matchesEnrichedFilter(f export.ExportFilter, a *ca.Analysis, t *stage.EntryStage) bool {
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

func formatErrorCode(code int) string {
	if code == 0 {
		return ""
	}
	return strconv.Itoa(code)
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
