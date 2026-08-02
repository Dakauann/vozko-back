package export_usecase

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"vozko/domain/analysis"
	"vozko/domain/export"
	"vozko/domain/lead"
	shared_domain "vozko/domain/shared"
	"vozko/domain/stage"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type exportEntriesUseCase struct {
	wcEntryRepo  wce.Repository
	leadRepo     lead.Repository
	analysisRepo analysis.Repository
	stageRepo    stage.Repository
	// listers supply channel-neutral rows for channels that have no campaigns.
	// Keyed by entry type so registering one never displaces another.
	listers map[export.EntryType]export.ChannelEntryLister
}

// SetChannelEntryLister registers a channel's export source.
//
// Without one, that channel's conversations cannot be exported at all — the old
// behaviour for everything except WhatsApp, which returned "unsupported entry
// type" and gave an operator no way to get their data out.
func (uc *exportEntriesUseCase) SetChannelEntryLister(entryType export.EntryType, lister export.ChannelEntryLister) {
	if uc == nil || lister == nil || entryType == "" {
		return
	}
	if uc.listers == nil {
		uc.listers = make(map[export.EntryType]export.ChannelEntryLister, 2)
	}
	uc.listers[entryType] = lister
}

func NewExportEntriesUseCase(
	wcEntryRepo wce.Repository,
	leadRepo lead.Repository,
	analysisRepo analysis.Repository,
	stageRepo stage.Repository,
) export.ExportEntriesUseCase {
	return &exportEntriesUseCase{
		wcEntryRepo:  wcEntryRepo,
		leadRepo:     leadRepo,
		analysisRepo: analysisRepo,
		stageRepo:    stageRepo,
	}
}

func (uc *exportEntriesUseCase) Export(filter export.ExportFilter, w io.Writer) (int, error) {
	if filter.EntryType == export.EntryTypeWhatsApp {
		// A WhatsApp conversation cannot exist without a campaign, so requiring
		// one here is a real invariant rather than an accident of the API shape.
		if filter.CampaignID == "" {
			return 0, fmt.Errorf("campaign id is required")
		}
		return uc.exportWhatsApp(filter, w)
	}

	lister, ok := uc.listers[filter.EntryType]
	if !ok {
		return 0, fmt.Errorf("unsupported entry type: %s", filter.EntryType)
	}
	if filter.WorkspaceID == "" {
		return 0, fmt.Errorf("workspace id is required")
	}
	return uc.exportChannel(filter, lister, w)
}

// exportChannel exports a channel with no campaign concept.
//
// CampaignID, when present, means the account: the container filter is the same
// question, and the caller already sends the account id in that slot everywhere
// else in the CRM.
func (uc *exportEntriesUseCase) exportChannel(filter export.ExportFilter, lister export.ChannelEntryLister, w io.Writer) (int, error) {
	entries, err := lister.ListForExport(filter.WorkspaceID, filter.CampaignID)
	if err != nil {
		return 0, fmt.Errorf("list entries: %w", err)
	}
	if len(entries) == 0 {
		return 0, nil
	}

	entryIDs := make([]string, len(entries))
	for i, e := range entries {
		entryIDs[i] = e.EntryID
	}

	// Analyses and stages key on (entry_id, entry_type), so they are read exactly
	// as they are for WhatsApp — no per-channel branch needed.
	analysisMap, err := uc.analysisRepo.FindLatestByEntries(entryIDs, shared_domain.EntryType(filter.EntryType))
	if err != nil {
		return 0, fmt.Errorf("load analyses: %w", err)
	}
	stageMap, err := uc.stageRepo.GetBatchEntryStages(entryIDs, string(filter.EntryType), filter.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("load tags: %w", err)
	}

	maxVars := 0
	for _, e := range entries {
		if len(e.Variables) > maxVars {
			maxVars = len(e.Variables)
		}
	}
	metaKeys := collectMetadataKeys(entries, func(e export.ChannelEntry) map[string]interface{} {
		return e.Metadata
	})

	var rows []export.ExportRow
	for _, e := range entries {
		a := analysisMap[e.EntryID]
		entryStage := stageMap[e.EntryID]

		row := export.ExportRow{
			Number:    e.Number,
			Name:      e.Name,
			Status:    e.Status,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			Variables: e.Variables,
			Metadata:  e.Metadata,
		}
		if entryStage != nil {
			row.StageName = entryStage.StageName
		}
		populateAnalysisFields(&row, a)

		if !matchesFilter(filter, row, a, entryStage) {
			continue
		}
		rows = append(rows, row)
	}

	return writeCSV(w, rows, maxVars, metaKeys, filter.EntryType)
}

func (uc *exportEntriesUseCase) exportWhatsApp(filter export.ExportFilter, w io.Writer) (int, error) {
	entries, err := uc.wcEntryRepo.ListByCampaignID(filter.CampaignID)
	if err != nil {
		return 0, fmt.Errorf("list entries: %w", err)
	}
	if len(entries) == 0 {
		return 0, nil
	}

	entryIDs := make([]string, len(entries))
	leadIDs := make([]string, 0, len(entries))
	leadIDSet := make(map[string]struct{})
	for i, e := range entries {
		entryIDs[i] = e.ID
		if _, ok := leadIDSet[e.LeadID]; !ok {
			leadIDSet[e.LeadID] = struct{}{}
			leadIDs = append(leadIDs, e.LeadID)
		}
	}

	leads, err := uc.leadRepo.FindByIDs(filter.WorkspaceID, leadIDs)
	if err != nil {
		return 0, fmt.Errorf("load leads: %w", err)
	}
	leadMap := make(map[string]*lead.Lead, len(leads))
	for _, l := range leads {
		leadMap[l.ID] = l
	}

	analysisMap, err := uc.analysisRepo.FindLatestByEntries(entryIDs, shared_domain.EntryTypeWhatsApp)
	if err != nil {
		return 0, fmt.Errorf("load analyses: %w", err)
	}

	stageMap, err := uc.stageRepo.GetBatchEntryStages(entryIDs, "whatsapp", filter.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("load tags: %w", err)
	}

	maxVars := 0
	for _, e := range entries {
		if len(e.Variables) > maxVars {
			maxVars = len(e.Variables)
		}
	}

	metaKeys := collectMetadataKeys(entries, func(e wce.WhatsAppCampaignEntry) map[string]interface{} {
		return e.Metadata
	})

	var rows []export.ExportRow
	for _, e := range entries {
		l := leadMap[e.LeadID]
		a := analysisMap[e.ID]
		entryTags := stageMap[e.ID]

		row := buildRowWhatsApp(&e, l, a, entryTags)

		if !matchesFilter(filter, row, a, entryTags) {
			continue
		}

		rows = append(rows, row)
	}

	return writeCSV(w, rows, maxVars, metaKeys, filter.EntryType)
}

func buildRowWhatsApp(e *wce.WhatsAppCampaignEntry, l *lead.Lead, a *analysis.Analysis, t *stage.EntryStage) export.ExportRow {
	row := export.ExportRow{
		Status:    string(e.Status),
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
		Variables: e.Variables,
		Metadata:  e.Metadata,
	}

	if l != nil {
		row.Number = l.Number
		row.Name = l.Name
		row.Age = l.Age
	}

	if t != nil {
		row.StageName = t.StageName
	}

	populateAnalysisFields(&row, a)
	return row
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

func matchesFilter(f export.ExportFilter, row export.ExportRow, a *analysis.Analysis, t *stage.EntryStage) bool {
	if f.Status != "" && !strings.EqualFold(row.Status, f.Status) {
		return false
	}
	if f.Number != "" && !strings.Contains(row.Number, f.Number) {
		return false
	}
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

func writeCSV(w io.Writer, rows []export.ExportRow, maxVars int, metaKeys []string, entryType export.EntryType) (int, error) {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	header := []string{"number", "name", "age", "status", "tag", "created_at", "updated_at"}

	if entryType == export.EntryTypeWhatsApp && maxVars > 0 {
		for i := 1; i <= maxVars; i++ {
			header = append(header, fmt.Sprintf("variable_%d", i))
		}
	}

	for _, k := range metaKeys {
		header = append(header, "meta_"+k)
	}

	header = append(header,
		"analysis_interest",
		"analysis_disposition",
		"analysis_sentiment",
		"analysis_qualification",
		"analysis_next_action",
		"analysis_attendance_quality",
		"analysis_summary",
		"analysis_product_interest",
	)

	if err := writer.Write(header); err != nil {
		return 0, fmt.Errorf("write header: %w", err)
	}

	for _, row := range rows {
		record := []string{
			formatNumber(row.Number),
			row.Name,
			formatOptionalInt(row.Age),
			row.Status,
			row.StageName,
			row.CreatedAt,
			row.UpdatedAt,
		}

		if entryType == export.EntryTypeWhatsApp && maxVars > 0 {
			for i := 0; i < maxVars; i++ {
				if i < len(row.Variables) {
					record = append(record, row.Variables[i])
				} else {
					record = append(record, "")
				}
			}
		}

		for _, k := range metaKeys {
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
			sanitizeCSVText(row.AnalysisSummary),
			row.AnalysisProductInterest,
		)

		if err := writer.Write(record); err != nil {
			return 0, fmt.Errorf("write row: %w", err)
		}
	}

	return len(rows), nil
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

func collectMetadataKeys[T any](entries []T, getMeta func(T) map[string]interface{}) []string {
	keySet := make(map[string]struct{})
	for _, e := range entries {
		m := getMeta(e)
		for k := range m {
			keySet[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}

	sortStrings(keys)
	return keys
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func formatMetaValue(v interface{}) string {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return sanitizeCSVText(string(b))
	default:
		return sanitizeCSVText(fmt.Sprintf("%v", v))
	}
}

func sanitizeCSVText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
