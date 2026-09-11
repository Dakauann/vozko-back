package comment_analysis_repository

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

// NewRepository builds the comment-analysis store.
func NewRepository(db *gorm.DB) ca.Repository {
	return &repository{db: db}
}

// Insert is ON CONFLICT DO NOTHING on (source, subject_kind,
// source_comment_id). That one clause is what makes webhook redelivery free: a
// subject delivered twice is classified (and billed) once.
//
// subject_kind is in the key because the id spaces overlap. Instagram carries
// both comments and conversations, so without it a conversation entry id could
// collide with a comment id and one of the two would silently never ingest.
func (r *repository) Insert(ctx context.Context, a *ca.CommentAnalysis) (bool, error) {
	row := fromDomain(a)
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source"}, {Name: "subject_kind"}, {Name: "source_comment_id"}},
			DoNothing: true,
		}).
		Create(row)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	a.ID = row.ID
	return true, nil
}

func (r *repository) FindByID(ctx context.Context, workspaceID, id string) (*ca.CommentAnalysis, error) {
	var row schema.CommentAnalysis
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) FindBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string) (*ca.CommentAnalysis, error) {
	var row schema.CommentAnalysis
	err := r.db.WithContext(ctx).
		Where("source = ? AND subject_kind = ? AND source_comment_id = ?", string(source), string(ca.SubjectKindComment), sourceCommentID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) ListPending(ctx context.Context, ref ca.ContainerRef, limit int) ([]*ca.CommentAnalysis, error) {
	if limit < 1 {
		limit = 1
	}
	var rows []schema.CommentAnalysis
	err := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL AND source = ? AND subject_kind = ? AND account_id = ? AND container_id = ?",
			string(ca.StatusPending), string(ref.Source), string(ref.Normalized().Kind), ref.AccountID, ref.ContainerID).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(rows), nil
}

// ClaimByIDs is ONE conditional write. The status guard is what makes two
// replicas that planned the same rows receive disjoint sets: only the first
// UPDATE observes pending. The attempt is counted in the same write, so a
// crash after this point still consumed a try and a crash loop terminates.
func (r *repository) ClaimByIDs(ctx context.Context, ids []string, now time.Time) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var claimed []string
	err := r.db.WithContext(ctx).Raw(`
		UPDATE comment_analyses
		   SET status = ?, attempts = attempts + 1, updated_at = ?
		 WHERE status = ? AND deleted_at IS NULL AND id IN ?
		RETURNING id`,
		string(ca.StatusInFlight), now, string(ca.StatusPending), ids,
	).Scan(&claimed).Error
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *repository) Save(ctx context.Context, a *ca.CommentAnalysis) error {
	return r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Where("id = ?", a.ID).
		Updates(saveColumns(a)).Error
}

func (r *repository) SaveMany(ctx context.Context, rows []*ca.CommentAnalysis) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, a := range rows {
			if err := tx.Model(&schema.CommentAnalysis{}).Where("id = ?", a.ID).Updates(saveColumns(a)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// saveColumns is the full mutable set, listed explicitly so a zero value
// (Severity 0, RequiresAction false, FailureReason "") is written rather
// than skipped, which is what GORM's struct Updates would do.
func saveColumns(a *ca.CommentAnalysis) map[string]any {
	var batchID *string
	if a.BatchID != "" {
		id := a.BatchID
		batchID = &id
	}
	return map[string]any{
		"status":             string(a.Status),
		"attempts":           a.Attempts,
		"failure_reason":     a.FailureReason,
		"sentiment":          string(a.Sentiment),
		"stance":             string(a.Stance),
		"intent":             string(a.Intent),
		"topic_key":          a.TopicKey,
		"is_spam":            a.IsSpam,
		"language":           a.Language,
		"toxicity":           string(a.Toxicity),
		"personal_attack":    string(a.PersonalAttack),
		"legal_risk":         string(a.LegalRisk),
		"severity":           a.Severity,
		"interest":           string(a.Interest),
		"product_interest":   a.ProductInterest,
		"disposition":        string(a.Disposition),
		"qualification":      string(a.Qualification),
		"next_action":        string(a.NextAction),
		"summary":            a.Summary,
		"attendance_quality": a.AttendanceQuality,
		"message_count":      a.MessageCount,
		"commented_at":       a.CommentedAt,
		"requires_action":    a.RequiresAction,
		"truncated":          a.Truncated,
		"batch_id":           batchID,
		"model":              a.Model,
		"analyzed_at":        a.AnalyzedAt,
		"updated_at":         a.UpdatedAt,
		"deleted_at":         a.DeletedAt,
	}
}

// ListPendingContainers is the backstop (§6.3): what is waiting, from the
// database alone.
func (r *repository) ListPendingContainers(ctx context.Context, olderThan time.Time, limit int) ([]ca.PendingContainer, error) {
	if limit < 1 {
		limit = 100
	}
	type row struct {
		SubjectKind string
		Source      string
		AccountID   string
		ContainerID string
		WorkspaceID string
		Pending     int
		OldestAt    time.Time
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Select(`subject_kind, source, account_id, container_id, MIN(workspace_id::text) AS workspace_id,
			COUNT(*) AS pending, MIN(created_at) AS oldest_at`).
		Where("status = ? AND deleted_at IS NULL AND created_at < ?", string(ca.StatusPending), olderThan).
		Group("subject_kind, source, account_id, container_id").
		Order("oldest_at ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ca.PendingContainer, 0, len(rows))
	for _, x := range rows {
		out = append(out, ca.PendingContainer{
			// The kind is grouped and carried, not defaulted: without it the
			// backstop would hand a container of conversations to the comment
			// adapter and fail every row it found.
			Ref: ca.ContainerRef{
				Kind: ca.SubjectKind(x.SubjectKind), Source: ca.Source(x.Source),
				AccountID: x.AccountID, ContainerID: x.ContainerID,
			}.Normalized(),
			WorkspaceID: x.WorkspaceID,
			Pending:     x.Pending,
			OldestAt:    x.OldestAt,
		})
	}
	return out, nil
}

func (r *repository) ListStaleInFlight(ctx context.Context, claimedBefore time.Time, limit int) ([]*ca.CommentAnalysis, error) {
	if limit < 1 {
		limit = 500
	}
	var rows []schema.CommentAnalysis
	err := r.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", string(ca.StatusInFlight), claimedBefore).
		Order("updated_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toDomainSlice(rows), nil
}

func (r *repository) CountPendingBySource(ctx context.Context) (map[ca.Source]int, error) {
	type row struct {
		Source string
		N      int
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Select("source, COUNT(*) AS n").
		Where("status = ? AND deleted_at IS NULL", string(ca.StatusPending)).
		Group("source").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[ca.Source]int, len(rows))
	for _, x := range rows {
		out[ca.Source(x.Source)] = x.N
	}
	return out, nil
}

// applyFilters is the ONE place ListInput becomes SQL, shared by List and
// GetStats so the feed and the numbers above it describe the same rows.
func applyFilters(q *gorm.DB, in ca.ListInput) *gorm.DB {
	q = q.Where("comment_analyses.workspace_id = ? AND comment_analyses.deleted_at IS NULL", in.WorkspaceID)
	if in.Source != "" {
		q = q.Where("comment_analyses.source = ?", string(in.Source))
	}
	if in.AccountID != "" {
		q = q.Where("comment_analyses.account_id = ?", in.AccountID)
	}
	if in.ContainerID != "" {
		q = q.Where("comment_analyses.container_id = ?", in.ContainerID)
	}
	if in.From != nil {
		q = q.Where("comment_analyses.commented_at >= ?", *in.From)
	}
	if in.To != nil {
		q = q.Where("comment_analyses.commented_at < ?", *in.To)
	}
	if len(in.Statuses) > 0 {
		statuses := make([]string, len(in.Statuses))
		for i, s := range in.Statuses {
			statuses[i] = string(s)
		}
		q = q.Where("comment_analyses.status IN ?", statuses)
	}
	if in.TopicKey != "" {
		q = q.Where("comment_analyses.topic_key = ?", in.TopicKey)
	}
	if in.Stance != "" {
		q = q.Where("comment_analyses.stance = ?", string(in.Stance))
	}
	if in.Sentiment != "" {
		q = q.Where("comment_analyses.sentiment = ?", string(in.Sentiment))
	}
	if in.Intent != "" {
		q = q.Where("comment_analyses.intent = ?", string(in.Intent))
	}
	if in.SeverityMin != nil {
		q = q.Where("comment_analyses.severity >= ?", *in.SeverityMin)
	}
	if in.SeverityMax != nil {
		q = q.Where("comment_analyses.severity <= ?", *in.SeverityMax)
	}
	if in.RequiresAction != nil {
		q = q.Where("comment_analyses.requires_action = ?", *in.RequiresAction)
	}
	if in.AuthorExternalID != "" {
		q = q.Where("comment_analyses.author_external_id = ?", in.AuthorExternalID)
	}
	if len(in.SubjectKinds) > 0 {
		kinds := make([]string, len(in.SubjectKinds))
		for i, k := range in.SubjectKinds {
			kinds[i] = string(k)
		}
		q = q.Where("comment_analyses.subject_kind IN ?", kinds)
	}
	if in.Interest != "" {
		q = q.Where("comment_analyses.interest = ?", string(in.Interest))
	}
	if in.Disposition != "" {
		q = q.Where("comment_analyses.disposition = ?", string(in.Disposition))
	}
	if in.Qualification != "" {
		q = q.Where("comment_analyses.qualification = ?", string(in.Qualification))
	}
	if in.NextAction != "" {
		q = q.Where("comment_analyses.next_action = ?", string(in.NextAction))
	}
	return q
}

func (r *repository) List(ctx context.Context, in ca.ListInput) (*shared.PaginatedResult[*ca.CommentAnalysis], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)
	q := applyFilters(r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}), in)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	order := "comment_analyses.commented_at DESC"
	for _, s := range in.Options.Sorts {
		switch s.Field {
		case "severity":
			order = "comment_analyses.severity " + direction(s.Direction) + ", comment_analyses.commented_at DESC"
		case "commentedAt", "commented_at":
			order = "comment_analyses.commented_at " + direction(s.Direction)
		}
	}

	var rows []schema.CommentAnalysis
	if err := q.Order(order).Limit(pagination.PageSize).Offset(pagination.Offset()).Find(&rows).Error; err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(toDomainSlice(rows), pagination, total), nil
}

// ListAuthorContainers answers "which posts does this person turn up on" with
// one GROUP BY over the comments we already store (§2).
//
// The count is `COUNT(DISTINCT container_id)` over the same predicate rather
// than a count of the grouped rows: a grouped query's Count would return the
// number of comments, and a table paging on posts would then claim a page count
// it cannot fill.
//
// Ordered by the author's most recent activity on each post, with the container
// id as the tiebreak, so paging cannot repeat or skip a post when someone
// commented on two of them in the same second.
func (r *repository) ListAuthorContainers(ctx context.Context, in ca.AuthorContainersInput) (*shared.PaginatedResult[*ca.AuthorContainer], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)

	scope := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
			Where("workspace_id = ? AND deleted_at IS NULL", in.WorkspaceID).
			Where("author_external_id = ?", in.AuthorExternalID)
		if in.Source != "" {
			q = q.Where("source = ?", string(in.Source))
		}
		if in.AccountID != "" {
			q = q.Where("account_id = ?", in.AccountID)
		}
		if in.From != nil {
			q = q.Where("commented_at >= ?", *in.From)
		}
		if in.To != nil {
			q = q.Where("commented_at < ?", *in.To)
		}
		return q
	}

	var total int64
	if err := scope().Distinct("container_id").Count(&total).Error; err != nil {
		return nil, err
	}

	type row struct {
		Source            string
		AccountID         string
		ContainerID       string
		Comments          int
		StanceSupporter   int
		StanceNeutral     int
		StanceCritic      int
		StanceHostile     int
		SeverityMax       int
		SeverityHighCount int
		FirstCommentedAt  time.Time
		LastCommentedAt   time.Time
	}
	var rows []row
	err := scope().
		Select(`MIN(source) AS source, MIN(account_id::text) AS account_id, container_id,
			COUNT(*) AS comments,
			COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'supporter') AS stance_supporter,
			COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'neutral') AS stance_neutral,
			COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'critic') AS stance_critic,
			COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'hostile') AS stance_hostile,
			COALESCE(MAX(severity), 0) AS severity_max,
			COUNT(*) FILTER (WHERE status = 'analyzed' AND severity >= ?) AS severity_high_count,
			MIN(commented_at) AS first_commented_at, MAX(commented_at) AS last_commented_at`,
			ca.HighSeverityThreshold).
		Group("container_id").
		Order("last_commented_at DESC, container_id ASC").
		Limit(pagination.PageSize).Offset(pagination.Offset()).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]*ca.AuthorContainer, 0, len(rows))
	for _, x := range rows {
		c := &ca.AuthorContainer{
			Source:      ca.Source(x.Source),
			AccountID:   x.AccountID,
			ContainerID: x.ContainerID,
			Comments:    x.Comments,
			Stances: ca.StanceMix{
				Supporter: x.StanceSupporter,
				Neutral:   x.StanceNeutral,
				Critic:    x.StanceCritic,
				Hostile:   x.StanceHostile,
			},
			SeverityMax:       x.SeverityMax,
			SeverityHighCount: x.SeverityHighCount,
			FirstCommentedAt:  x.FirstCommentedAt,
			LastCommentedAt:   x.LastCommentedAt,
		}
		c.Derive()
		items = append(items, c)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func direction(d shared.SortDirection) string {
	if d == shared.SortAsc {
		return "ASC"
	}
	return "DESC"
}

// countersSelect is the §11.1 set as one COUNT(*) FILTER per column, the
// same technique the conversation analysis GetStats uses.
//
// author_external_id is table-qualified because the windowed author ranking
// joins comment_analysis_authors, which has a column of the same name; every
// other column here exists only on comment_analyses. Qualifying it is harmless
// for the callers that do not join.
const countersSelect = `
	COUNT(*) AS total,
	COUNT(*) FILTER (WHERE status = 'analyzed') AS analyzed,
	COUNT(*) FILTER (WHERE status = 'pending') AS pending,
	COUNT(*) FILTER (WHERE status = 'in_flight') AS in_flight,
	COUNT(*) FILTER (WHERE status = 'failed') AS failed,
	COUNT(*) FILTER (WHERE status = 'skipped') AS skipped,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND sentiment = 'positive') AS sentiment_positive,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND sentiment = 'neutral') AS sentiment_neutral,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND sentiment = 'negative') AS sentiment_negative,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'supporter') AS stance_supporter,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'neutral') AS stance_neutral,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'critic') AS stance_critic,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND stance = 'hostile') AS stance_hostile,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'praise') AS intent_praise,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'question') AS intent_question,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'complaint') AS intent_complaint,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'support_request') AS intent_support_request,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'spam') AS intent_spam,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'sales_lead') AS intent_sales_lead,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND intent = 'other') AS intent_other,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND is_spam) AS spam_count,
	COALESCE(AVG(severity) FILTER (WHERE status = 'analyzed'), 0) AS severity_avg,
	COALESCE(MAX(severity) FILTER (WHERE status = 'analyzed'), 0) AS severity_max,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND severity >= 60) AS severity_high_count,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND requires_action) AS requires_action_count,
	COUNT(DISTINCT comment_analyses.author_external_id) AS distinct_authors,
	COUNT(*) FILTER (WHERE subject_kind = 'comment') AS comment_count,
	COUNT(*) FILTER (WHERE subject_kind = 'conversation') AS conversation_count,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND interest = 'interested') AS interest_interested,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND interest = 'not_interested') AS interest_not_interested,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND interest = 'undecided') AS interest_undecided,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'sale') AS disposition_sale,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'filling_info') AS disposition_filling_info,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'callback') AS disposition_callback,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'declined') AS disposition_declined,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'no_answer') AS disposition_no_answer,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'voicemail') AS disposition_voicemail,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND disposition = 'pending') AS disposition_pending,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND qualification = 'hot_lead') AS qualification_hot_lead,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND qualification = 'warm_lead') AS qualification_warm_lead,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND qualification = 'cold_lead') AS qualification_cold_lead,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND next_action = 'schedule_callback') AS next_action_schedule_callback,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND next_action = 'send_whatsapp') AS next_action_send_whatsapp,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND next_action = 'close') AS next_action_close,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND next_action = 'escalate') AS next_action_escalate,
	COUNT(*) FILTER (WHERE status = 'analyzed' AND next_action = 'continue') AS next_action_continue,
	COALESCE(AVG(attendance_quality) FILTER (WHERE status = 'analyzed' AND subject_kind = 'conversation'), 0) AS attendance_quality_avg,
	COALESCE(MIN(attendance_quality) FILTER (WHERE status = 'analyzed' AND subject_kind = 'conversation'), 0) AS attendance_quality_min,
	COALESCE(MAX(attendance_quality) FILTER (WHERE status = 'analyzed' AND subject_kind = 'conversation'), 0) AS attendance_quality_max,
	COALESCE(SUM(message_count) FILTER (WHERE subject_kind = 'conversation'), 0) AS messages_total,
	COALESCE(AVG(message_count) FILTER (WHERE subject_kind = 'conversation'), 0) AS messages_avg`

type CountersRow struct {
	Total, Analyzed, Pending, InFlight, Failed, Skipped                  int
	SentimentPositive, SentimentNeutral, SentimentNegative               int
	StanceSupporter, StanceNeutral, StanceCritic, StanceHostile          int
	IntentPraise, IntentQuestion, IntentComplaint, IntentSupportRequest  int
	IntentSpam, IntentSalesLead, IntentOther, SpamCount                  int
	SeverityAvg                                                          float64
	SeverityMax, SeverityHighCount, RequiresActionCount, DistinctAuthors int

	CommentCount, ConversationCount                                     int
	InterestInterested, InterestNotInterested, InterestUndecided        int
	DispositionSale, DispositionFillingInfo, DispositionCallback        int
	DispositionDeclined, DispositionNoAnswer, DispositionVoicemail      int
	DispositionPending                                                  int
	QualificationHotLead, QualificationWarmLead, QualificationColdLead  int
	NextActionScheduleCallback, NextActionSendWhatsApp, NextActionClose int
	NextActionEscalate, NextActionContinue                              int
	AttendanceQualityAvg                                                float64
	AttendanceQualityMin, AttendanceQualityMax, MessagesTotal           int
	MessagesAvg                                                         float64
}

func (c CountersRow) counters() ca.Counters {
	return ca.Counters{
		Total: c.Total, Analyzed: c.Analyzed, Pending: c.Pending, InFlight: c.InFlight, Failed: c.Failed, Skipped: c.Skipped,
		SentimentPositive: c.SentimentPositive, SentimentNeutral: c.SentimentNeutral, SentimentNegative: c.SentimentNegative,
		StanceSupporter: c.StanceSupporter, StanceNeutral: c.StanceNeutral, StanceCritic: c.StanceCritic, StanceHostile: c.StanceHostile,
		IntentPraise: c.IntentPraise, IntentQuestion: c.IntentQuestion, IntentComplaint: c.IntentComplaint,
		IntentSupportRequest: c.IntentSupportRequest, IntentSpam: c.IntentSpam, IntentSalesLead: c.IntentSalesLead, IntentOther: c.IntentOther,
		SpamCount: c.SpamCount, SeverityAvg: c.SeverityAvg, SeverityMax: c.SeverityMax, SeverityHighCount: c.SeverityHighCount,
		RequiresActionCount: c.RequiresActionCount, DistinctAuthors: c.DistinctAuthors,

		CommentCount: c.CommentCount, ConversationCount: c.ConversationCount,
		InterestInterested: c.InterestInterested, InterestNotInterested: c.InterestNotInterested,
		InterestUndecided: c.InterestUndecided,
		DispositionSale:   c.DispositionSale, DispositionFillingInfo: c.DispositionFillingInfo,
		DispositionCallback: c.DispositionCallback, DispositionDeclined: c.DispositionDeclined,
		DispositionNoAnswer: c.DispositionNoAnswer, DispositionVoicemail: c.DispositionVoicemail,
		DispositionPending:   c.DispositionPending,
		QualificationHotLead: c.QualificationHotLead, QualificationWarmLead: c.QualificationWarmLead,
		QualificationColdLead:      c.QualificationColdLead,
		NextActionScheduleCallback: c.NextActionScheduleCallback, NextActionSendWhatsApp: c.NextActionSendWhatsApp,
		NextActionClose: c.NextActionClose, NextActionEscalate: c.NextActionEscalate,
		NextActionContinue:   c.NextActionContinue,
		AttendanceQualityAvg: c.AttendanceQualityAvg, AttendanceQualityMin: c.AttendanceQualityMin,
		AttendanceQualityMax: c.AttendanceQualityMax,
		MessagesTotal:        c.MessagesTotal, MessagesAvg: c.MessagesAvg,
	}
}

func (r *repository) GetStats(ctx context.Context, in ca.ListInput) (*ca.Stats, error) {
	var cr CountersRow
	if err := applyFilters(r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}), in).
		Select(countersSelect).Scan(&cr).Error; err != nil {
		return nil, err
	}
	stats := &ca.Stats{Counters: cr.counters(), Topics: []ca.TopicStat{}}

	type topicRow struct {
		TopicKey                                                      string
		Count, SentimentPositive, SentimentNeutral, SentimentNegative int
		SeverityAvg                                                   float64
	}
	var topics []topicRow
	if err := applyFilters(r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}), in).
		Select(`topic_key, COUNT(*) AS count,
			COUNT(*) FILTER (WHERE sentiment = 'positive') AS sentiment_positive,
			COUNT(*) FILTER (WHERE sentiment = 'neutral') AS sentiment_neutral,
			COUNT(*) FILTER (WHERE sentiment = 'negative') AS sentiment_negative,
			COALESCE(AVG(severity), 0) AS severity_avg`).
		Where("comment_analyses.status = ? AND comment_analyses.topic_key <> ''", string(ca.StatusAnalyzed)).
		Group("topic_key").
		Order("count DESC").
		Scan(&topics).Error; err != nil {
		return nil, err
	}
	for _, t := range topics {
		stats.Topics = append(stats.Topics, ca.TopicStat{
			TopicKey: t.TopicKey, Count: t.Count,
			SentimentPositive: t.SentimentPositive, SentimentNeutral: t.SentimentNeutral, SentimentNegative: t.SentimentNegative,
			SeverityAvg: t.SeverityAvg,
		})
	}

	// Flagged authors come from the projection, scoped the same way.
	fq := r.db.WithContext(ctx).Model(&schema.CommentAnalysisAuthor{}).
		Where("workspace_id = ? AND is_flagged = true", in.WorkspaceID)
	if in.Source != "" {
		fq = fq.Where("source = ?", string(in.Source))
	}
	if in.AccountID != "" {
		fq = fq.Where("account_id = ?", in.AccountID)
	}
	var flagged int64
	if err := fq.Count(&flagged).Error; err != nil {
		return nil, err
	}
	stats.FlaggedAuthors = int(flagged)
	return stats, nil
}

// AggregateAuthors counts, per author, over the account's rows. The
// derivation (stance, flag) is the domain's; this only fills Counters.
func (r *repository) AggregateAuthors(ctx context.Context, source ca.Source, accountID string, changedSince time.Time) ([]*ca.AuthorStats, error) {
	type row struct {
		CountersRow
		WorkspaceID      string
		AuthorExternalID string
		AuthorHandle     string
		FirstSeenAt      time.Time
		LastSeenAt       time.Time
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Select(`MIN(workspace_id::text) AS workspace_id, author_external_id,
			(ARRAY_AGG(author_handle ORDER BY commented_at DESC))[1] AS author_handle,
			MIN(commented_at) AS first_seen_at, MAX(commented_at) AS last_seen_at,`+countersSelect).
		Where("source = ? AND account_id = ? AND deleted_at IS NULL AND author_external_id <> ''", string(source), accountID).
		Where(`author_external_id IN (
			SELECT DISTINCT author_external_id FROM comment_analyses
			 WHERE source = ? AND account_id = ? AND updated_at >= ?)`, string(source), accountID, changedSince).
		Group("author_external_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Top topics per author, in one query; the top 3 are cut in Go.
	authorIDs := make([]string, len(rows))
	for i, x := range rows {
		authorIDs[i] = x.AuthorExternalID
	}
	type topicRow struct {
		AuthorExternalID string
		TopicKey         string
		Count            int
	}
	var topicRows []topicRow
	err = r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Select("author_external_id, topic_key, COUNT(*) AS count").
		Where("source = ? AND account_id = ? AND deleted_at IS NULL AND status = ? AND topic_key <> ''", string(source), accountID, string(ca.StatusAnalyzed)).
		Where("author_external_id IN ?", authorIDs).
		Group("author_external_id, topic_key").
		Scan(&topicRows).Error
	if err != nil {
		return nil, err
	}
	topicsByAuthor := make(map[string][]ca.TopicCount, len(rows))
	for _, t := range topicRows {
		topicsByAuthor[t.AuthorExternalID] = append(topicsByAuthor[t.AuthorExternalID], ca.TopicCount{TopicKey: t.TopicKey, Count: t.Count})
	}

	out := make([]*ca.AuthorStats, 0, len(rows))
	for _, x := range rows {
		top := topicsByAuthor[x.AuthorExternalID]
		sort.SliceStable(top, func(i, j int) bool { return top[i].Count > top[j].Count })
		if len(top) > 3 {
			top = top[:3]
		}
		if top == nil {
			top = []ca.TopicCount{}
		}
		out = append(out, &ca.AuthorStats{
			WorkspaceID:      x.WorkspaceID,
			Source:           source,
			AccountID:        accountID,
			AuthorExternalID: x.AuthorExternalID,
			AuthorHandle:     x.AuthorHandle,
			FirstSeenAt:      x.FirstSeenAt,
			LastSeenAt:       x.LastSeenAt,
			Counters:         x.CountersRow.counters(),
			TopTopics:        top,
		})
	}
	return out, nil
}

// AggregateRollups computes the three scopes for one UTC day by
// commented_at. Soft-deleted rows are deliberately included.
func (r *repository) AggregateRollups(ctx context.Context, day time.Time) ([]*ca.Rollup, error) {
	day = ca.BucketDate(day)
	next := day.Add(24 * time.Hour)

	type row struct {
		CountersRow
		WorkspaceID string
		Source      string
		AccountID   string
		ScopeID     string
	}
	scopes := []struct {
		scope   ca.RollupScope
		scopeID string
		extra   string
	}{
		{ca.ScopeAccount, "account_id", ""},
		{ca.ScopeContainer, "container_id", ""},
		{ca.ScopeTopic, "account_id || ':' || topic_key", "AND topic_key <> ''"},
	}

	var out []*ca.Rollup
	for _, s := range scopes {
		var rows []row
		err := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
			Select(`MIN(workspace_id::text) AS workspace_id, source, account_id, `+s.scopeID+` AS scope_id,`+countersSelect).
			Where("status = ? AND commented_at >= ? AND commented_at < ? "+s.extra, string(ca.StatusAnalyzed), day, next).
			Group("source, account_id, " + s.scopeID).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, x := range rows {
			out = append(out, &ca.Rollup{
				WorkspaceID: x.WorkspaceID,
				Source:      ca.Source(x.Source),
				AccountID:   x.AccountID,
				Scope:       s.scope,
				ScopeID:     x.ScopeID,
				BucketDate:  day,
				Counters:    x.CountersRow.counters(),
			})
		}
	}
	return out, nil
}

func (r *repository) DaysAnalyzedSince(ctx context.Context, since time.Time) ([]time.Time, error) {
	type row struct{ Day time.Time }
	var rows []row
	err := r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Select("DISTINCT date_trunc('day', commented_at AT TIME ZONE 'UTC') AS day").
		Where("analyzed_at >= ?", since).
		Order("day ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	days := make([]time.Time, 0, len(rows))
	for _, x := range rows {
		days = append(days, ca.BucketDate(x.Day))
	}
	return days, nil
}

// SoftDeleteBySourceComment tombstones a deleted comment. Scoped to the comment
// kind because that is what addresses a subject this way: the channel's
// "comment deleted" webhook. A conversation is never removed by id from
// outside, and leaving the kind out would let a colliding entry id tombstone
// the wrong row.
func (r *repository) SoftDeleteBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.CommentAnalysis{}).
		Where("source = ? AND subject_kind = ? AND source_comment_id = ? AND deleted_at IS NULL",
			string(source), string(ca.SubjectKindComment), sourceCommentID).
		Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
}

// PurgeBefore deletes in bounded slices so retention never holds a long
// lock on the table the flush job is writing to.
func (r *repository) PurgeBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit < 1 {
		limit = 1000
	}
	res := r.db.WithContext(ctx).Exec(`
		DELETE FROM comment_analyses
		 WHERE id IN (
		       SELECT id FROM comment_analyses
		        WHERE created_at < ? AND status <> ?
		        ORDER BY created_at ASC
		        LIMIT ?)`, cutoff, string(ca.StatusInFlight), limit)
	return res.RowsAffected, res.Error
}
