package choicestats_repository

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/choicestats"
)

const counterKey = `workspace_id, source_kind, source_id, node_id, version, option_id, day, shard`

const recordSendSQL = `WITH inserted AS (
	INSERT INTO choice_prompts (prompt_key, workspace_id, source_kind, source_id, node_id, version, template_id, provider_message_id, sent_day, sent_at, shard, clicked_option_id)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '')
	ON CONFLICT (prompt_key) DO NOTHING
	RETURNING workspace_id, source_kind, source_id, node_id, version, template_id, sent_day, shard
)
INSERT INTO choice_daily_counts (` + counterKey + `, template_id, shown, clicks)
SELECT i.workspace_id, i.source_kind, i.source_id, i.node_id, i.version, o.option_id, i.sent_day, i.shard, i.template_id, 1, 0
FROM inserted i CROSS JOIN unnest(?::text[]) AS o(option_id)
ON CONFLICT (` + counterKey + `)
DO UPDATE SET shown = choice_daily_counts.shown + 1`

const (
	clickByKey     = `p.prompt_key = ?`
	clickByMessage = `p.provider_message_id = ?`
)

const shownOptionOfTheCohort = `
	AND EXISTS (
		SELECT 1 FROM choice_daily_counts c
		WHERE c.workspace_id = p.workspace_id AND c.source_kind = p.source_kind AND c.source_id = p.source_id
		AND c.node_id = p.node_id AND c.version = p.version AND c.option_id = ? AND c.day = p.sent_day AND c.shard = p.shard
	)`

func clickSQL(target string, mustBeShown bool) string {
	shown := ""
	if mustBeShown {
		shown = shownOptionOfTheCohort
	}
	return `WITH clicked AS (
	UPDATE choice_prompts p SET clicked_option_id = ?, clicked_at = ?
	WHERE p.workspace_id = ? AND ` + target + ` AND p.clicked_option_id = '' AND p.sent_at >= ?` + shown + `
	RETURNING p.workspace_id, p.source_kind, p.source_id, p.node_id, p.version, p.template_id, p.sent_day, p.shard
)
INSERT INTO choice_daily_counts (` + counterKey + `, template_id, shown, clicks)
SELECT k.workspace_id, k.source_kind, k.source_id, k.node_id, k.version, o.option_id, k.sent_day, k.shard, k.template_id, 0, 1
FROM clicked k CROSS JOIN unnest(?::text[]) AS o(option_id)
ON CONFLICT (` + counterKey + `)
DO UPDATE SET clicks = choice_daily_counts.clicks + 1`
}

func sourceCountsSQL(oneVersion bool) string {
	version := ""
	if oneVersion {
		version = ` AND version = ?`
	}
	return `SELECT node_id, option_id, SUM(shown) AS shown, SUM(clicks) AS clicks
FROM choice_daily_counts
WHERE workspace_id = ? AND source_kind = ? AND source_id = ? AND day >= ? AND day <= ?` + version + `
GROUP BY node_id, option_id`
}

const templateCountsSQL = `SELECT '' AS node_id, option_id, SUM(shown) AS shown, SUM(clicks) AS clicks
FROM choice_daily_counts
WHERE workspace_id = ? AND template_id = ? AND template_id <> '' AND day >= ? AND day <= ?
GROUP BY option_id`

const prunePromptsSQL = `DELETE FROM choice_prompts
WHERE prompt_key IN (
	SELECT prompt_key FROM choice_prompts
	WHERE sent_at < ?
	ORDER BY sent_at
	LIMIT ?
)`

const pruneCountsSQL = `DELETE FROM choice_daily_counts
WHERE ctid IN (
	SELECT ctid FROM choice_daily_counts
	WHERE day < ?
	LIMIT ?
)`

var errCountScopeMissing = errors.New("choice stats: a count names a source or a template")

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

var _ choicestats.Store = (*Store)(nil)

func (s *Store) RecordSend(ctx context.Context, send choicestats.SendRecord) (bool, error) {
	res := s.db.WithContext(ctx).Exec(recordSendSQL,
		send.PromptKey, send.WorkspaceID, string(send.Source.Kind), send.Source.ID, send.Source.NodeID, send.Source.Version,
		send.Source.TemplateID, send.ProviderMessageID, send.SentDay, send.SentAt, send.Shard,
		pq.Array(send.CounterOptions))
	return res.RowsAffected > 0, res.Error
}

func (s *Store) RecordClick(ctx context.Context, click choicestats.ClickRecord) (bool, error) {
	target, ref := clickByKey, click.PromptKey
	if click.PromptKey == "" {
		target, ref = clickByMessage, click.ProviderMessageID
	}
	mustBeShown := click.Option != choicestats.OptionFreeText
	args := []any{click.Option, click.At, click.WorkspaceID, ref, click.NotBefore}
	if mustBeShown {
		args = append(args, click.Option)
	}
	args = append(args, pq.Array(click.CounterOptions))
	res := s.db.WithContext(ctx).Exec(clickSQL(target, mustBeShown), args...)
	return res.RowsAffected > 0, res.Error
}

type countRow struct {
	NodeID   string
	OptionID string
	Shown    int64
	Clicks   int64
}

func (s *Store) Counts(ctx context.Context, q choicestats.CountQuery) ([]choicestats.CountRow, error) {
	var sql string
	var args []any
	switch {
	case q.TemplateID != "":
		sql, args = templateCountsSQL, []any{q.WorkspaceID, q.TemplateID, q.From, q.To}
	case q.Kind != "" && q.SourceID != "":
		sql, args = sourceCountsSQL(q.Version != nil), []any{q.WorkspaceID, string(q.Kind), q.SourceID, q.From, q.To}
		if q.Version != nil {
			args = append(args, *q.Version)
		}
	default:
		return nil, errCountScopeMissing
	}
	var rows []countRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]choicestats.CountRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, choicestats.CountRow{NodeID: row.NodeID, OptionID: row.OptionID, Shown: row.Shown, Clicks: row.Clicks})
	}
	return out, nil
}

func (s *Store) PrunePrompts(before time.Time, limit int) (int64, error) {
	res := s.db.Exec(prunePromptsSQL, before, limit)
	return res.RowsAffected, res.Error
}

func (s *Store) PruneCounts(before time.Time, limit int) (int64, error) {
	res := s.db.Exec(pruneCountsSQL, before, limit)
	return res.RowsAffected, res.Error
}
