package teamchat_repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/teamchat"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const clientIndex = "ux_team_chat_messages_client"

type Repository struct {
	db *gorm.DB
}

var _ teamchat.Repository = (*Repository)(nil)

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateThread(ctx context.Context, t teamchat.Thread, members []teamchat.Member, notice *teamchat.Message) (teamchat.Thread, bool, error) {
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := threadRowOf(t)
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		created = true
		if err := upsertMembers(tx, members); err != nil {
			return err
		}
		if notice != nil {
			_, _, err := appendIn(tx, *notice)
			return err
		}
		return nil
	})
	if err != nil {
		return teamchat.Thread{}, false, err
	}
	if !created {
		existing, err := r.FindDirect(ctx, t.WorkspaceID, t.DirectKey)
		return existing, false, err
	}
	stored, err := r.FindThread(ctx, t.WorkspaceID, t.ID)
	return stored, true, err
}

func (r *Repository) FindThread(ctx context.Context, workspaceID, id string) (teamchat.Thread, error) {
	if !readableIDs(workspaceID, id) {
		return teamchat.Thread{}, teamchat.ErrNotFound
	}
	return r.takeThread(ctx, "workspace_id = ? AND id = ?", workspaceID, id)
}

func (r *Repository) FindDirect(ctx context.Context, workspaceID, directKey string) (teamchat.Thread, error) {
	if !readableIDs(workspaceID) || directKey == "" {
		return teamchat.Thread{}, teamchat.ErrNotFound
	}
	return r.takeThread(ctx, "workspace_id = ? AND direct_key = ?", workspaceID, directKey)
}

func (r *Repository) takeThread(ctx context.Context, where string, args ...interface{}) (teamchat.Thread, error) {
	var row schema.TeamChatThread
	err := r.db.WithContext(ctx).Where(where, args...).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return teamchat.Thread{}, teamchat.ErrNotFound
	}
	if err != nil {
		return teamchat.Thread{}, err
	}
	return threadOf(row), nil
}

func (r *Repository) Roster(ctx context.Context, threadID string) (teamchat.Roster, error) {
	rosters, err := r.Rosters(ctx, []string{threadID})
	if err != nil {
		return nil, err
	}
	return rosters[threadID], nil
}

func (r *Repository) Rosters(ctx context.Context, threadIDs []string) (map[string]teamchat.Roster, error) {
	out := make(map[string]teamchat.Roster, len(threadIDs))
	ids := onlyUUIDs(threadIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []schema.TeamChatMember
	if err := r.db.WithContext(ctx).Where("thread_id IN ?", ids).Order("joined_at, user_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ThreadID] = append(out[row.ThreadID], memberOf(row))
	}
	return out, nil
}

type threadListRow struct {
	schema.TeamChatThread
	MeRole        string
	MeLastReadSeq int64
	MeMuted       bool
	MeJoinedAt    time.Time
}

func (r *Repository) ListThreads(ctx context.Context, q teamchat.ThreadQuery) ([]teamchat.ThreadRow, error) {
	if !readableIDs(q.WorkspaceID, q.UserID) {
		return []teamchat.ThreadRow{}, nil
	}
	query := r.db.WithContext(ctx).
		Table("team_chat_members m").
		Select("t.*, m.role AS me_role, m.last_read_seq AS me_last_read_seq, m.muted AS me_muted, m.joined_at AS me_joined_at").
		Joins("JOIN team_chat_threads t ON t.id = m.thread_id").
		Where("m.workspace_id = ? AND m.user_id = ? AND m.left_at IS NULL", q.WorkspaceID, q.UserID)
	if q.After != nil {
		query = query.Where("(t.last_message_at, t.id) < (?, ?)", q.After.At.UTC(), q.After.ID)
	}
	var rows []threadListRow
	if err := query.Order("t.last_message_at DESC, t.id DESC").Limit(q.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]teamchat.ThreadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, teamchat.ThreadRow{
			Thread: threadOf(row.TeamChatThread),
			Me: teamchat.Member{
				ThreadID:    row.ID,
				WorkspaceID: row.WorkspaceID,
				UserID:      q.UserID,
				Role:        teamchat.Role(row.MeRole),
				LastReadSeq: row.MeLastReadSeq,
				Muted:       row.MeMuted,
				JoinedAt:    row.MeJoinedAt.UTC(),
			},
		})
	}
	return out, nil
}

func (r *Repository) ChangeMembers(ctx context.Context, t teamchat.Thread, members []teamchat.Member, notice teamchat.Message) (teamchat.Message, error) {
	var stored teamchat.Message
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := upsertMembers(tx, members); err != nil {
			return err
		}
		var err error
		stored, _, err = appendIn(tx, notice)
		return err
	})
	return stored, err
}

func (r *Repository) Rename(ctx context.Context, t teamchat.Thread, notice teamchat.Message) (teamchat.Message, error) {
	var stored teamchat.Message
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&schema.TeamChatThread{}).
			Where("id = ? AND workspace_id = ?", t.ID, t.WorkspaceID).
			Update("title", t.Title)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return teamchat.ErrNotFound
		}
		var err error
		stored, _, err = appendIn(tx, notice)
		return err
	})
	return stored, err
}

func (r *Repository) MarkRead(ctx context.Context, threadID, userID string, seq int64) (int64, error) {
	if !readableIDs(threadID, userID) {
		return 0, teamchat.ErrNotFound
	}
	var read []int64
	err := r.db.WithContext(ctx).Raw(`
		UPDATE team_chat_members m
		SET last_read_seq = GREATEST(m.last_read_seq, LEAST(?, t.last_seq))
		FROM team_chat_threads t
		WHERE t.id = m.thread_id AND m.thread_id = ? AND m.user_id = ? AND m.left_at IS NULL
		RETURNING m.last_read_seq`, seq, threadID, userID).Scan(&read).Error
	if err != nil {
		return 0, err
	}
	if len(read) == 0 {
		return 0, teamchat.ErrNotFound
	}
	return read[0], nil
}

func (r *Repository) SetMuted(ctx context.Context, threadID, userID string, muted bool) error {
	if !readableIDs(threadID, userID) {
		return teamchat.ErrNotFound
	}
	result := r.db.WithContext(ctx).Model(&schema.TeamChatMember{}).
		Where("thread_id = ? AND user_id = ? AND left_at IS NULL", threadID, userID).
		Update("muted", muted)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return teamchat.ErrNotFound
	}
	return nil
}

const unreadTotalsQuery = `
	SELECT
		COUNT(*) FILTER (WHERE t.last_seq > m.last_read_seq) AS unread_threads,
		COALESCE(SUM(GREATEST(t.last_seq - m.last_read_seq, 0)), 0) AS unread_messages
	FROM team_chat_members m
	JOIN team_chat_threads t ON t.id = m.thread_id
	WHERE m.workspace_id = ? AND m.user_id = ? AND m.left_at IS NULL
		AND (NOT m.muted OR EXISTS (
			SELECT 1 FROM team_chat_messages x
			WHERE x.thread_id = m.thread_id AND x.seq > m.last_read_seq
				AND x.deleted_at IS NULL AND CAST(? AS uuid) = ANY(x.mentions)
		))`

func (r *Repository) UnreadTotals(ctx context.Context, workspaceID, userID string) (teamchat.Totals, error) {
	if !readableIDs(workspaceID, userID) {
		return teamchat.Totals{}, nil
	}
	var row struct {
		UnreadThreads  int
		UnreadMessages int64
	}
	if err := r.db.WithContext(ctx).Raw(unreadTotalsQuery, workspaceID, userID, userID).Scan(&row).Error; err != nil {
		return teamchat.Totals{}, err
	}
	return teamchat.Totals{UnreadThreads: row.UnreadThreads, UnreadMessages: row.UnreadMessages}, nil
}

const unreadMentionsQuery = `
	SELECT x.thread_id, COUNT(*) AS mentions
	FROM team_chat_messages x
	JOIN team_chat_members m ON m.thread_id = x.thread_id AND m.user_id = ?
	WHERE x.thread_id IN ? AND x.seq > m.last_read_seq
		AND x.deleted_at IS NULL AND CAST(? AS uuid) = ANY(x.mentions)
	GROUP BY x.thread_id`

func (r *Repository) UnreadMentions(ctx context.Context, userID string, threadIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	ids := onlyUUIDs(threadIDs)
	if !readableIDs(userID) || len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ThreadID string
		Mentions int64
	}
	if err := r.db.WithContext(ctx).Raw(unreadMentionsQuery, userID, ids, userID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ThreadID] = row.Mentions
	}
	return out, nil
}

func (r *Repository) Append(ctx context.Context, m teamchat.Message) (teamchat.Message, bool, error) {
	var stored teamchat.Message
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		stored, created, err = appendIn(tx, m)
		return err
	})
	if err != nil && m.ClientID != "" && database.IsUniqueViolationOf(err, clientIndex) {
		existing, findErr := findByClientID(r.db.WithContext(ctx), m)
		return existing, false, findErr
	}
	return stored, created, err
}

func appendIn(tx *gorm.DB, m teamchat.Message) (teamchat.Message, bool, error) {
	if m.ClientID != "" {
		existing, err := findByClientID(tx, m)
		if err == nil {
			return existing, false, nil
		}
		if !errors.Is(err, teamchat.ErrMessageNotFound) {
			return teamchat.Message{}, false, err
		}
	}
	var seqs []int64
	err := tx.Raw(`
		UPDATE team_chat_threads SET last_seq = last_seq + 1, last_message_at = ?
		WHERE id = ? AND workspace_id = ?
		RETURNING last_seq`, m.CreatedAt.UTC(), m.ThreadID, m.WorkspaceID).Scan(&seqs).Error
	if err != nil {
		return teamchat.Message{}, false, err
	}
	if len(seqs) == 0 {
		return teamchat.Message{}, false, teamchat.ErrNotFound
	}
	m.Seq = seqs[0]
	row := messageRowOf(m)
	if err := tx.Create(&row).Error; err != nil {
		return teamchat.Message{}, false, err
	}
	err = tx.Exec(`
		UPDATE team_chat_members SET last_read_seq = GREATEST(last_read_seq, ?)
		WHERE thread_id = ? AND user_id = ? AND left_at IS NULL`, m.Seq, m.ThreadID, m.AuthorID).Error
	if err != nil {
		return teamchat.Message{}, false, err
	}
	return m, true, nil
}

func findByClientID(db *gorm.DB, m teamchat.Message) (teamchat.Message, error) {
	var row schema.TeamChatMessage
	err := db.Where("thread_id = ? AND author_id = ? AND client_id = ?", m.ThreadID, m.AuthorID, m.ClientID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return teamchat.Message{}, teamchat.ErrMessageNotFound
	}
	if err != nil {
		return teamchat.Message{}, err
	}
	return messageOf(row), nil
}

func (r *Repository) ListMessages(ctx context.Context, q teamchat.MessageQuery) ([]teamchat.Message, error) {
	if !readableIDs(q.ThreadID) {
		return []teamchat.Message{}, nil
	}
	query := r.db.WithContext(ctx).Where("thread_id = ?", q.ThreadID)
	ascending := q.AfterSeq > 0
	switch {
	case ascending:
		query = query.Where("seq > ?", q.AfterSeq).Order("seq ASC")
	case q.BeforeSeq > 0:
		query = query.Where("seq < ?", q.BeforeSeq).Order("seq DESC")
	default:
		query = query.Order("seq DESC")
	}
	var rows []schema.TeamChatMessage
	if err := query.Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]teamchat.Message, len(rows))
	for i, row := range rows {
		if ascending {
			out[i] = messageOf(row)
		} else {
			out[len(rows)-1-i] = messageOf(row)
		}
	}
	return out, nil
}

func (r *Repository) LastMessages(ctx context.Context, threads []teamchat.Thread) (map[string]teamchat.Message, error) {
	out := map[string]teamchat.Message{}
	pairs := make([][]interface{}, 0, len(threads))
	for _, t := range threads {
		if t.LastSeq > 0 && readableIDs(t.ID) {
			pairs = append(pairs, []interface{}{t.ID, t.LastSeq})
		}
	}
	if len(pairs) == 0 {
		return out, nil
	}
	var rows []schema.TeamChatMessage
	if err := r.db.WithContext(ctx).Where("(thread_id, seq) IN ?", pairs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ThreadID] = messageOf(row)
	}
	return out, nil
}

func (r *Repository) FindMessage(ctx context.Context, workspaceID, id string) (teamchat.Message, error) {
	if !readableIDs(workspaceID, id) {
		return teamchat.Message{}, teamchat.ErrMessageNotFound
	}
	var row schema.TeamChatMessage
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return teamchat.Message{}, teamchat.ErrMessageNotFound
	}
	if err != nil {
		return teamchat.Message{}, err
	}
	return messageOf(row), nil
}

func (r *Repository) SaveMessage(ctx context.Context, m teamchat.Message) error {
	result := r.db.WithContext(ctx).Model(&schema.TeamChatMessage{}).
		Where("workspace_id = ? AND id = ?", m.WorkspaceID, m.ID).
		Updates(map[string]interface{}{
			"body":       m.Body,
			"mentions":   stringArray(m.Mentions),
			"edited_at":  m.EditedAt,
			"deleted_at": m.DeletedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return teamchat.ErrMessageNotFound
	}
	return nil
}

func (r *Repository) CreateScheduled(ctx context.Context, s teamchat.Scheduled) error {
	row := scheduledRowOf(s)
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *Repository) FindScheduled(ctx context.Context, id string) (teamchat.Scheduled, error) {
	if !readableIDs(id) {
		return teamchat.Scheduled{}, teamchat.ErrScheduledNotFound
	}
	var row schema.TeamChatScheduledMessage
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return teamchat.Scheduled{}, teamchat.ErrScheduledNotFound
	}
	if err != nil {
		return teamchat.Scheduled{}, err
	}
	return scheduledOf(row), nil
}

func (r *Repository) ListScheduled(ctx context.Context, threadID, authorID string) ([]teamchat.Scheduled, error) {
	if !readableIDs(threadID, authorID) {
		return []teamchat.Scheduled{}, nil
	}
	var rows []schema.TeamChatScheduledMessage
	err := r.db.WithContext(ctx).
		Where("thread_id = ? AND author_id = ? AND status = ?", threadID, authorID, string(teamchat.ScheduledPending)).
		Order("send_at, id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return scheduledList(rows), nil
}

func (r *Repository) SaveScheduled(ctx context.Context, s teamchat.Scheduled) error {
	result := r.db.WithContext(ctx).Model(&schema.TeamChatScheduledMessage{}).
		Where("id = ? AND status = ?", s.ID, string(teamchat.ScheduledPending)).
		Updates(map[string]interface{}{
			"body":            s.Body,
			"send_at":         s.SendAt.UTC(),
			"status":          string(s.Status),
			"failure_reason":  string(s.FailureReason),
			"sent_message_id": optionalID(s.SentMessageID),
			"updated_at":      s.UpdatedAt.UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return teamchat.ErrScheduledFinal
	}
	return nil
}

func (r *Repository) DueScheduled(ctx context.Context, now time.Time, limit int) ([]teamchat.Scheduled, error) {
	var rows []schema.TeamChatScheduledMessage
	err := r.db.WithContext(ctx).
		Where("status = ? AND send_at <= ?", string(teamchat.ScheduledPending), now.UTC()).
		Order("send_at, id").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return scheduledList(rows), nil
}

func upsertMembers(tx *gorm.DB, members []teamchat.Member) error {
	if len(members) == 0 {
		return nil
	}
	rows := make([]schema.TeamChatMember, 0, len(members))
	for _, m := range members {
		rows = append(rows, memberRowOf(m))
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "thread_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "last_read_seq", "muted", "joined_at", "left_at"}),
	}).Create(&rows).Error
}

func readableIDs(ids ...string) bool {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return false
		}
	}
	return true
}

func onlyUUIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if readableIDs(id) {
			out = append(out, id)
		}
	}
	return out
}
