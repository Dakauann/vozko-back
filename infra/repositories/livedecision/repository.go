package livedecision_repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type readStore struct{ db *gorm.DB }

func NewReadStore(db *gorm.DB) ld.ReadStore { return &readStore{db: db} }

func (s *readStore) Save(ctx context.Context, read ld.LiveRead) (bool, error) {
	certainty, err := json.Marshal(read.Certainty)
	if err != nil {
		return false, err
	}
	row := schema.ConversationLiveRead{
		EntryID:           read.EntryID,
		EntryType:         read.EntryType,
		WorkspaceID:       read.WorkspaceID,
		Interest:          string(read.Interest),
		Disposition:       string(read.Disposition),
		Sentiment:         string(read.Sentiment),
		Qualification:     string(read.Qualification),
		NextAction:        string(read.NextAction),
		Language:          read.Language,
		AttendanceQuality: read.AttendanceQuality,
		Certainty:         certainty,
		StageSettled:      read.StageSettled,
		DecidedThrough:    read.DecidedThrough,
		DecidedAt:         read.DecidedAt,
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "entry_id"}, {Name: "entry_type"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"workspace_id", "interest", "disposition", "sentiment", "qualification", "next_action", "language",
			"attendance_quality", "certainty", "stage_settled", "decided_through", "decided_at",
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "conversation_live_reads.decided_through <= excluded.decided_through AND conversation_live_reads.workspace_id = excluded.workspace_id"},
		}},
	}).Create(&row)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (s *readStore) Get(ctx context.Context, workspaceID, entryID, entryType string) (*ld.LiveRead, error) {
	var row schema.ConversationLiveRead
	err := s.db.WithContext(ctx).
		Where("workspace_id = ? AND entry_id = ? AND entry_type = ?", workspaceID, entryID, entryType).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	read := toLiveRead(row)
	return &read, nil
}

func (s *readStore) ForEntries(ctx context.Context, workspaceID string, refs []ld.EntryRef) (map[ld.EntryRef]ld.LiveRead, error) {
	out := make(map[ld.EntryRef]ld.LiveRead, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(refs))
	wanted := make(map[ld.EntryRef]bool, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.EntryID)
		wanted[ref] = true
	}
	var rows []schema.ConversationLiveRead
	if err := s.db.WithContext(ctx).Where("workspace_id = ? AND entry_id IN ?", workspaceID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		ref := ld.EntryRef{EntryID: row.EntryID, EntryType: row.EntryType}
		if wanted[ref] {
			out[ref] = toLiveRead(row)
		}
	}
	return out, nil
}

func toLiveRead(row schema.ConversationLiveRead) ld.LiveRead {
	certainty := map[string]float64{}
	_ = json.Unmarshal(row.Certainty, &certainty)
	return ld.LiveRead{
		WorkspaceID:       row.WorkspaceID,
		EntryID:           row.EntryID,
		EntryType:         row.EntryType,
		Interest:          audience.Interest(row.Interest),
		Disposition:       audience.Disposition(row.Disposition),
		Sentiment:         shared.Sentiment(row.Sentiment),
		Qualification:     audience.Qualification(row.Qualification),
		NextAction:        audience.NextAction(row.NextAction),
		Language:          row.Language,
		AttendanceQuality: row.AttendanceQuality,
		Certainty:         certainty,
		StageSettled:      row.StageSettled,
		DecidedThrough:    row.DecidedThrough,
		DecidedAt:         row.DecidedAt,
	}
}

type decisionLog struct{ db *gorm.DB }

func NewLog(db *gorm.DB) ld.Log { return &decisionLog{db: db} }

func (l *decisionLog) Append(ctx context.Context, record ld.Record) error {
	answers, err := json.Marshal(answersForStorage(record.Answers))
	if err != nil {
		return err
	}
	effects := record.Effects
	if effects == nil {
		effects = []string{}
	}
	effectsJSON, err := json.Marshal(effects)
	if err != nil {
		return err
	}
	id := record.ID
	if id == "" {
		id = uuid.NewString()
	}
	return l.db.WithContext(ctx).Create(&schema.LiveDecisionRecord{
		ID:            id,
		WorkspaceID:   record.WorkspaceID,
		EntryID:       record.EntryID,
		EntryType:     record.EntryType,
		Purpose:       record.Purpose,
		Answers:       answers,
		Effects:       effectsJSON,
		Failure:       truncate(record.Failure, 500),
		Model:         record.Model,
		InputTokens:   record.InputTokens,
		CostMicros:    record.CostMicros,
		LatencyMillis: record.LatencyMillis,
		CreatedAt:     record.CreatedAt,
	}).Error
}

type storedAnswer struct {
	Kind       decision.Kind `json:"kind"`
	Choice     string        `json:"choice,omitempty"`
	Confidence float64       `json:"confidence,omitempty"`
	Yes        float64       `json:"yes,omitempty"`
	Score      float64       `json:"score,omitempty"`
}

func answersForStorage(answers map[string]decision.Answer) map[string]storedAnswer {
	out := make(map[string]storedAnswer, len(answers))
	for id, a := range answers {
		out[id] = storedAnswer{Kind: a.Kind, Choice: a.Choice, Confidence: a.Confidence, Yes: a.Yes, Score: a.Score}
	}
	return out
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func (l *decisionLog) Summarize(ctx context.Context, since time.Time) ([]ld.Summary, error) {
	var totals []struct {
		WorkspaceID   string
		WorkspaceName string
		Decisions     int
		Failures      int
		CostMicros    int64
		AvgLatency    float64
	}
	err := l.db.WithContext(ctx).Raw(`
		SELECT r.workspace_id, COALESCE(w.name, '') AS workspace_name,
		       COUNT(*) AS decisions,
		       COUNT(*) FILTER (WHERE r.failure <> '') AS failures,
		       COALESCE(SUM(r.cost_micros), 0) AS cost_micros,
		       COALESCE(AVG(r.latency_millis) FILTER (WHERE r.failure = ''), 0) AS avg_latency
		  FROM live_decision_records r
		  LEFT JOIN workspaces w ON w.id = r.workspace_id
		 WHERE r.created_at >= ?
		 GROUP BY r.workspace_id, w.name
		 ORDER BY decisions DESC`, since).Scan(&totals).Error
	if err != nil {
		return nil, err
	}
	var effects []struct {
		WorkspaceID string
		Effect      string
		Total       int
	}
	err = l.db.WithContext(ctx).Raw(`
		SELECT r.workspace_id, e.effect, COUNT(*) AS total
		  FROM live_decision_records r
		  CROSS JOIN LATERAL jsonb_array_elements_text(r.effects) AS e(effect)
		 WHERE r.created_at >= ?
		 GROUP BY r.workspace_id, e.effect`, since).Scan(&effects).Error
	if err != nil {
		return nil, err
	}
	byWorkspace := make(map[string]map[string]int, len(totals))
	for _, e := range effects {
		if byWorkspace[e.WorkspaceID] == nil {
			byWorkspace[e.WorkspaceID] = map[string]int{}
		}
		byWorkspace[e.WorkspaceID][e.Effect] = e.Total
	}
	out := make([]ld.Summary, 0, len(totals))
	for _, t := range totals {
		counts := byWorkspace[t.WorkspaceID]
		if counts == nil {
			counts = map[string]int{}
		}
		out = append(out, ld.Summary{
			WorkspaceID:   t.WorkspaceID,
			WorkspaceName: t.WorkspaceName,
			Decisions:     t.Decisions,
			Failures:      t.Failures,
			CostMicros:    t.CostMicros,
			AvgLatency:    time.Duration(t.AvgLatency * float64(time.Millisecond)),
			Effects:       counts,
		})
	}
	return out, nil
}
