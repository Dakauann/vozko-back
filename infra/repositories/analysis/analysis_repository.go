package analysis_repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/analysis"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

var allowedAnalysisSortFields = map[string]string{
	"created_at":         "created_at",
	"createdat":          "created_at",
	"attendance_quality": "attendance_quality",
	"attendancequality":  "attendance_quality",
	"interest":           "interest",
	"disposition":        "disposition",
	"sentiment":          "sentiment",
	"qualification":      "qualification",
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) analysis.Repository {
	return &repository{db: db}
}

func (r *repository) Create(a *analysis.Analysis) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}

	record := mapToSchema(a)
	if err := r.db.Create(&record).Error; err != nil {
		return err
	}

	a.CreatedAt = record.CreatedAt
	return nil
}

func (r *repository) FindByID(id string) (*analysis.Analysis, error) {
	var record schema.Analysis
	if err := r.db.Where("id = ?", id).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) FindLatestByEntry(entryID string, entryType shared.EntryType) (*analysis.Analysis, error) {
	var record schema.Analysis
	if err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Order("created_at DESC").
		First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) FindLatestByEntries(entryIDs []string, entryType shared.EntryType) (map[string]*analysis.Analysis, error) {
	if len(entryIDs) == 0 {
		return make(map[string]*analysis.Analysis), nil
	}

	var records []schema.Analysis
	if err := r.db.Raw(`
		SELECT DISTINCT ON (entry_id) *
		FROM analyses
		WHERE entry_id IN (?) AND entry_type = ?
		ORDER BY entry_id, created_at DESC
	`, entryIDs, string(entryType)).Scan(&records).Error; err != nil {
		return nil, err
	}

	result := make(map[string]*analysis.Analysis, len(records))
	for i := range records {
		a := mapToDomain(&records[i])
		result[a.EntryID] = a
	}
	return result, nil
}

func (r *repository) ListByEntry(entryID string, entryType shared.EntryType) ([]analysis.Analysis, error) {
	var records []schema.Analysis
	if err := r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	result := make([]analysis.Analysis, len(records))
	for i := range records {
		result[i] = *mapToDomain(&records[i])
	}
	return result, nil
}

func (r *repository) ListByLeadID(leadID string) ([]analysis.Analysis, error) {
	var records []schema.Analysis
	if err := r.db.Where(`
		entry_type = 'whatsapp' AND entry_id IN (
			SELECT id FROM whatsapp_campaign_entries WHERE lead_id = ? AND deleted_at IS NULL
		)
	`, leadID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	result := make([]analysis.Analysis, len(records))
	for i := range records {
		result[i] = *mapToDomain(&records[i])
	}
	return result, nil
}

func (r *repository) ListByWhatsAppCampaign(whatsappCampaignID string) ([]analysis.Analysis, error) {
	var records []schema.Analysis
	if err := r.db.Where(`
		entry_type = 'whatsapp' AND entry_id IN (
			SELECT id FROM whatsapp_campaign_entries WHERE campaign_id = ? AND deleted_at IS NULL
		)
	`, whatsappCampaignID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	result := make([]analysis.Analysis, len(records))
	for i := range records {
		result[i] = *mapToDomain(&records[i])
	}
	return result, nil
}

func (r *repository) DeleteByEntry(entryID string, entryType shared.EntryType) error {
	return r.db.Where("entry_id = ? AND entry_type = ?", entryID, string(entryType)).Delete(&schema.Analysis{}).Error
}

func (r *repository) buildLatestAnalysisSubquery(input analysis.ListAnalysisInput) *gorm.DB {
	latestQuery := r.db.Model(&schema.Analysis{}).
		Select("entry_id, entry_type, MAX(created_at) as max_created_at").
		Group("entry_id, entry_type")

	if input.WhatsAppCampaignID != "" {
		latestQuery = latestQuery.Where(`
			entry_type = 'whatsapp' AND entry_id IN (
				SELECT id FROM whatsapp_campaign_entries WHERE campaign_id = ? AND deleted_at IS NULL
			)`, input.WhatsAppCampaignID)
	}

	if input.LeadID != "" {
		latestQuery = latestQuery.Where(`
			entry_type = 'whatsapp' AND EXISTS (
				SELECT 1 FROM whatsapp_campaign_entries wce_l WHERE wce_l.id = analyses.entry_id AND wce_l.lead_id = ? AND wce_l.deleted_at IS NULL
			)
		`, input.LeadID)
	}

	if input.WorkspaceID != "" {
		latestQuery = latestQuery.Where(`
			(entry_type = 'whatsapp' AND EXISTS (
				SELECT 1 FROM whatsapp_campaign_entries wce
				JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.deleted_at IS NULL
				WHERE wce.id = analyses.entry_id AND wc.workspace_id = ? AND wce.deleted_at IS NULL
			))
		`, input.WorkspaceID)
	}

	if input.EntryType.Valid() {
		latestQuery = latestQuery.Where("entry_type = ?", string(input.EntryType))
	}

	return latestQuery
}

func (r *repository) GetStats(input analysis.ListAnalysisInput) (*analysis.AnalysisStats, error) {
	latestSubquery := r.buildLatestAnalysisSubquery(input)

	baseQuery := r.db.Model(&schema.Analysis{}).
		Joins(`INNER JOIN (?) AS latest ON analyses.entry_id = latest.entry_id 
			AND analyses.entry_type = latest.entry_type 
			AND analyses.created_at = latest.max_created_at`, latestSubquery)

	baseQuery = r.applyAnalysisFilters(baseQuery, input)

	type combinedResult struct {
		TotalAnalyses        int64
		AvgAttendanceQuality float64
		MinAttendanceQuality int
		MaxAttendanceQuality int
		TotalMessages        int64

		InterestInterested    int
		InterestNotInterested int
		InterestUndecided     int

		DispositionSale        int
		DispositionFillingInfo int
		DispositionCallback    int
		DispositionDeclined    int
		DispositionNoAnswer    int
		DispositionVoicemail   int
		DispositionPending     int

		SentimentPositive int
		SentimentNeutral  int
		SentimentNegative int

		QualificationHotLead  int
		QualificationWarmLead int
		QualificationColdLead int
	}

	var stats combinedResult
	if err := baseQuery.Select(`
		COUNT(*) as total_analyses,
		COALESCE(AVG(analyses.attendance_quality), 0) as avg_attendance_quality,
		COALESCE(MIN(analyses.attendance_quality), 0) as min_attendance_quality,
		COALESCE(MAX(analyses.attendance_quality), 0) as max_attendance_quality,
		COALESCE(SUM(analyses.message_count), 0) as total_messages,
		-- Interest counts
		COUNT(*) FILTER (WHERE analyses.interest = 'interested') as interest_interested,
		COUNT(*) FILTER (WHERE analyses.interest = 'not_interested') as interest_not_interested,
		COUNT(*) FILTER (WHERE analyses.interest = 'undecided') as interest_undecided,
		-- Disposition counts
		COUNT(*) FILTER (WHERE analyses.disposition = 'sale') as disposition_sale,
		COUNT(*) FILTER (WHERE analyses.disposition = 'filling_info') as disposition_filling_info,
		COUNT(*) FILTER (WHERE analyses.disposition = 'callback') as disposition_callback,
		COUNT(*) FILTER (WHERE analyses.disposition = 'declined') as disposition_declined,
		COUNT(*) FILTER (WHERE analyses.disposition = 'no_answer') as disposition_no_answer,
		COUNT(*) FILTER (WHERE analyses.disposition = 'voicemail') as disposition_voicemail,
		COUNT(*) FILTER (WHERE analyses.disposition = 'pending') as disposition_pending,
		-- Sentiment counts
		COUNT(*) FILTER (WHERE analyses.sentiment = 'positive') as sentiment_positive,
		COUNT(*) FILTER (WHERE analyses.sentiment = 'neutral') as sentiment_neutral,
		COUNT(*) FILTER (WHERE analyses.sentiment = 'negative') as sentiment_negative,
		-- Qualification counts
		COUNT(*) FILTER (WHERE analyses.qualification = 'hot_lead') as qualification_hot_lead,
		COUNT(*) FILTER (WHERE analyses.qualification = 'warm_lead') as qualification_warm_lead,
		COUNT(*) FILTER (WHERE analyses.qualification = 'cold_lead') as qualification_cold_lead
	`).Scan(&stats).Error; err != nil {
		return nil, err
	}

	result := &analysis.AnalysisStats{
		TotalAnalyses:        int(stats.TotalAnalyses),
		AvgAttendanceQuality: stats.AvgAttendanceQuality,
		MinAttendanceQuality: stats.MinAttendanceQuality,
		MaxAttendanceQuality: stats.MaxAttendanceQuality,
		TotalMessages:        int(stats.TotalMessages),

		InterestInterested:    stats.InterestInterested,
		InterestNotInterested: stats.InterestNotInterested,
		InterestUndecided:     stats.InterestUndecided,

		DispositionSale:        stats.DispositionSale,
		DispositionFillingInfo: stats.DispositionFillingInfo,
		DispositionCallback:    stats.DispositionCallback,
		DispositionDeclined:    stats.DispositionDeclined,
		DispositionNoAnswer:    stats.DispositionNoAnswer,
		DispositionVoicemail:   stats.DispositionVoicemail,
		DispositionPending:     stats.DispositionPending,

		SentimentPositive: stats.SentimentPositive,
		SentimentNeutral:  stats.SentimentNeutral,
		SentimentNegative: stats.SentimentNegative,

		QualificationHotLead:  stats.QualificationHotLead,
		QualificationWarmLead: stats.QualificationWarmLead,
		QualificationColdLead: stats.QualificationColdLead,
	}

	if stats.TotalAnalyses > 0 {
		result.AvgMessagesPerAnalysis = float64(stats.TotalMessages) / float64(stats.TotalAnalyses)
	}

	return result, nil
}

func (r *repository) applyAnalysisFilters(query *gorm.DB, input analysis.ListAnalysisInput) *gorm.DB {
	if input.Interest.Valid() {
		query = query.Where("analyses.interest = ?", string(input.Interest))
	}
	if input.Disposition.Valid() {
		query = query.Where("analyses.disposition = ?", string(input.Disposition))
	}
	if input.Sentiment.Valid() {
		query = query.Where("analyses.sentiment = ?", string(input.Sentiment))
	}
	if input.Qualification.Valid() {
		query = query.Where("analyses.qualification = ?", string(input.Qualification))
	}
	if input.NextAction.Valid() {
		query = query.Where("analyses.next_action = ?", string(input.NextAction))
	}
	if input.AttendanceQualityMin != nil {
		query = query.Where("analyses.attendance_quality >= ?", *input.AttendanceQualityMin)
	}
	if input.AttendanceQualityMax != nil {
		query = query.Where("analyses.attendance_quality <= ?", *input.AttendanceQualityMax)
	}
	if input.MessageCountMin != nil {
		query = query.Where("analyses.message_count >= ?", *input.MessageCountMin)
	}
	if input.MessageCountMax != nil {
		query = query.Where("analyses.message_count <= ?", *input.MessageCountMax)
	}
	return query
}

func (r *repository) List(input analysis.ListAnalysisInput) (*shared.PaginatedResult[*analysis.Analysis], error) {
	query := r.db.Model(&schema.Analysis{})
	query = r.applyFilters(query, input)

	var totalItems int64
	if err := query.Count(&totalItems).Error; err != nil {
		return nil, err
	}

	pagination := shared.NormalizePagination(input.Options.Pagination)
	offset := pagination.Offset()

	query = r.applySorts(query, input.Options.Sorts)
	query = query.Offset(offset).Limit(pagination.PageSize)

	var records []schema.Analysis
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*analysis.Analysis, len(records))
	for i := range records {
		items[i] = mapToDomain(&records[i])
	}

	return shared.NewPaginatedResult(items, pagination, totalItems), nil
}

func (r *repository) applyFilters(query *gorm.DB, input analysis.ListAnalysisInput) *gorm.DB {
	if input.WhatsAppCampaignID != "" {
		query = query.Where(`
			entry_type = 'whatsapp' AND entry_id IN (
				SELECT id FROM whatsapp_campaign_entries WHERE campaign_id = ? AND deleted_at IS NULL
			)
		`, input.WhatsAppCampaignID)
	}

	if input.LeadID != "" {
		query = query.Where(`
			entry_type = 'whatsapp' AND entry_id IN (
				SELECT id FROM whatsapp_campaign_entries WHERE lead_id = ? AND deleted_at IS NULL
			)
		`, input.LeadID)
	}

	if input.WorkspaceID != "" {
		query = query.Where(`
			(entry_type = 'whatsapp' AND entry_id IN (
				SELECT wce.id FROM whatsapp_campaign_entries wce
				JOIN whatsapp_campaigns wc ON wc.id = wce.campaign_id AND wc.deleted_at IS NULL
				WHERE wc.workspace_id = ? AND wce.deleted_at IS NULL
			))
		`, input.WorkspaceID)
	}

	if input.EntryType.Valid() {
		query = query.Where("entry_type = ?", string(input.EntryType))
	}

	if input.Interest != "" {
		query = query.Where("interest = ?", string(input.Interest))
	}

	if input.Disposition != "" {
		query = query.Where("disposition = ?", string(input.Disposition))
	}

	if input.Sentiment != "" {
		query = query.Where("sentiment = ?", string(input.Sentiment))
	}

	if input.Qualification != "" {
		query = query.Where("qualification = ?", string(input.Qualification))
	}

	if input.NextAction != "" {
		query = query.Where("next_action = ?", string(input.NextAction))
	}

	if input.AttendanceQualityMin != nil {
		query = query.Where("attendance_quality >= ?", *input.AttendanceQualityMin)
	}

	if input.AttendanceQualityMax != nil {
		query = query.Where("attendance_quality <= ?", *input.AttendanceQualityMax)
	}

	return query
}

func (r *repository) applySorts(query *gorm.DB, sorts []shared.Sort) *gorm.DB {
	applied := false
	for _, sort := range sorts {
		col, ok := allowedAnalysisSortFields[strings.ToLower(strings.TrimSpace(sort.Field))]
		if !ok {
			continue
		}
		dir := "ASC"
		if sort.Direction == shared.SortDesc {
			dir = "DESC"
		}
		query = query.Order(col + " " + dir)
		applied = true
	}
	if !applied {
		return query.Order("created_at DESC")
	}
	return query
}

func mapToSchema(a *analysis.Analysis) schema.Analysis {
	return schema.Analysis{
		ID:                a.ID,
		EntryID:           a.EntryID,
		EntryType:         string(a.EntryType),
		Interest:          string(a.Interest),
		ProductInterest:   a.ProductInterest,
		Disposition:       string(a.Disposition),
		Sentiment:         string(a.Sentiment),
		Qualification:     string(a.Qualification),
		NextAction:        string(a.NextAction),
		Summary:           a.Summary,
		AttendanceQuality: a.AttendanceQuality,
		MessageCount:      a.MessageCount,
		CreatedAt:         a.CreatedAt,
	}
}

func mapToDomain(record *schema.Analysis) *analysis.Analysis {
	return &analysis.Analysis{
		ID:                record.ID,
		EntryID:           record.EntryID,
		EntryType:         shared.EntryType(record.EntryType),
		Interest:          analysis.Interest(record.Interest),
		ProductInterest:   record.ProductInterest,
		Disposition:       analysis.Disposition(record.Disposition),
		Sentiment:         analysis.Sentiment(record.Sentiment),
		Qualification:     analysis.Qualification(record.Qualification),
		NextAction:        analysis.NextAction(record.NextAction),
		Summary:           record.Summary,
		AttendanceQuality: record.AttendanceQuality,
		MessageCount:      record.MessageCount,
		CreatedAt:         record.CreatedAt,
	}
}
