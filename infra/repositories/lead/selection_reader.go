package lead

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
	infracrmfilter "vozko/infra/repositories/crmfilter"
)

const (
	selectionReadTimeout   = "30s"
	selectionFreezeTimeout = "60s"
)

var (
	selectionReadSettings   = []string{"SET LOCAL jit = off", "SET LOCAL statement_timeout = '" + selectionReadTimeout + "'"}
	selectionFreezeSettings = []string{"SET LOCAL jit = off", "SET LOCAL statement_timeout = '" + selectionFreezeTimeout + "'"}
)

const (
	snapshotLockSQL  = "SELECT pg_advisory_xact_lock(hashtextextended(?::text, 0))"
	snapshotPageSQL  = "SELECT lead_id FROM lead_selection_snapshots WHERE snapshot_id = ?::uuid AND workspace_id = ?::uuid"
	snapshotDropSQL  = "DELETE FROM lead_selection_snapshots WHERE snapshot_id = ?::uuid AND workspace_id = ?::uuid"
	snapshotCountSQL = "SELECT COUNT(*) FROM lead_selection_snapshots WHERE snapshot_id = ?::uuid AND workspace_id = ?::uuid"
	snapshotSweepSQL = "DELETE FROM lead_selection_snapshots WHERE ctid IN (SELECT kept.ctid FROM lead_selection_snapshots kept WHERE kept.created_at < ?" +
		" AND NOT EXISTS (SELECT 1 FROM lead_action_runs r WHERE r.id = kept.snapshot_id AND r.status IN ('queued', 'running'))" +
		" AND NOT EXISTS (SELECT 1 FROM report_jobs j WHERE j.workspace_id = kept.workspace_id AND j.kind = 'leads' AND j.status IN ('queued', 'running') AND j.params ->> 'snapshotId' = kept.snapshot_id::text)" +
		" AND NOT EXISTS (SELECT 1 FROM call_lists cl WHERE cl.id = kept.snapshot_id AND cl.status = 'building')" +
		" LIMIT ?)"
)

type selectionQuery struct {
	list  *listQuery
	where string
	args  []interface{}
	order string
	limit int
}

func (r *repository) selection(sq lead.SelectionQuery) (*selectionQuery, error) {
	if err := sq.Validate(); err != nil {
		return nil, err
	}
	q, err := r.compile(lead.ListLeadsInput{WorkspaceID: sq.WorkspaceID, Filter: sq.Filter, Today: sq.Today})
	if err != nil {
		return nil, err
	}
	s := &selectionQuery{list: q, where: q.where, args: append([]interface{}{}, q.args...), limit: sq.Limit}
	if len(sq.IDs) > 0 {
		s.where += " AND leads.id = ANY(?::uuid[])"
		s.args = append(s.args, pq.StringArray(sq.IDs))
	}
	if len(sq.ExcludeIDs) > 0 {
		s.where += " AND NOT (leads.id = ANY(?::uuid[]))"
		s.args = append(s.args, pq.StringArray(sq.ExcludeIDs))
	}
	if !sq.Require.IsEmpty() {
		frag, args, err := infracrmfilter.Compile(sq.Require, q.desc, 0)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", lead.ErrLeadFilterInvalid, err)
		}
		s.where += " AND (" + frag + ")"
		s.args = append(s.args, args...)
	}
	if sq.Pending != nil {
		b, err := newBulkEdit(leadaction.Edit{Kind: sq.Pending.Kind, Key: sq.Pending.Key, Value: sq.Pending.Value})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", lead.ErrAssignmentInvalid, err)
		}
		unchanged, args := b.unchanged()
		s.where += " AND NOT (" + unchanged + ")"
		s.args = append(s.args, args...)
	}
	if sq.Ordered() {
		s.order = orderBy(q.desc, sq.Order)
	}
	return s, nil
}

func (s *selectionQuery) countSQL() (string, []interface{}) {
	return "SELECT COUNT(*) FROM leads WHERE " + s.where, s.args
}

func (s *selectionQuery) chosen() (string, []interface{}) {
	return "SELECT leads.id FROM leads WHERE " + s.where + " ORDER BY " + s.order + " LIMIT ?", append(append([]interface{}{}, s.args...), s.limit)
}

func (s *selectionQuery) pageSQL(after string, limit int) (string, []interface{}) {
	if s.order != "" {
		head, args := s.chosen()
		sql := "SELECT chosen.id FROM (" + head + ") chosen"
		if after != "" {
			sql += " WHERE chosen.id > ?::uuid"
			args = append(args, after)
		}
		return sql + " ORDER BY chosen.id LIMIT ?", append(args, limit)
	}
	sql := "SELECT leads.id FROM leads WHERE " + s.where
	args := append([]interface{}{}, s.args...)
	if after != "" {
		sql += " AND leads.id > ?::uuid"
		args = append(args, after)
	}
	return sql + " ORDER BY leads.id LIMIT ?", append(args, limit)
}

func (s *selectionQuery) freezeSQL(snapshotID string, at time.Time) (string, []interface{}) {
	insert := "INSERT INTO lead_selection_snapshots (snapshot_id, workspace_id, lead_id, created_at) SELECT ?::uuid, leads.workspace_id, leads.id, ? FROM leads WHERE " + s.where
	args := append([]interface{}{snapshotID, at}, s.args...)
	if s.order != "" {
		insert += " ORDER BY " + s.order + " LIMIT ?"
		args = append(args, s.limit)
	}
	return insert + " ON CONFLICT DO NOTHING", args
}

func (r *repository) inSelectionSession(ctx context.Context, s *selectionQuery, fn func(tx *gorm.DB) error) error {
	return inReadSession(ctx, r.db, s.list.sessionSettings(selectionReadSettings), fn)
}

func (r *repository) CountSelection(ctx context.Context, sq lead.SelectionQuery) (int, error) {
	s, err := r.selection(sq)
	if err != nil {
		return 0, err
	}
	sql, args := s.countSQL()
	var total int64
	if err := r.inSelectionSession(ctx, s, func(tx *gorm.DB) error { return tx.Raw(sql, args...).Scan(&total).Error }); err != nil {
		return 0, fmt.Errorf("count lead selection: %w", err)
	}
	return int(total), nil
}

func (r *repository) SelectionPage(ctx context.Context, sq lead.SelectionQuery, after string, limit int) ([]string, error) {
	s, err := r.selection(sq)
	if err != nil {
		return nil, err
	}
	if after, err = keysetCursor(after); err != nil {
		return nil, err
	}
	sql, args := s.pageSQL(after, limit)
	ids := []string{}
	if err := r.inSelectionSession(ctx, s, func(tx *gorm.DB) error { return tx.Raw(sql, args...).Scan(&ids).Error }); err != nil {
		return nil, fmt.Errorf("read lead selection page: %w", err)
	}
	return ids, nil
}

func (r *repository) FreezeSelection(ctx context.Context, sq lead.SelectionQuery, snapshotID string) (lead.Frozen, error) {
	snapshotID, err := snapshotRef(snapshotID)
	if err != nil {
		return lead.Frozen{}, err
	}
	s, err := r.selection(sq)
	if err != nil {
		return lead.Frozen{}, err
	}
	var frozen lead.Frozen
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range s.list.sessionSettings(selectionFreezeSettings) {
			if err := tx.Exec(setting).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(snapshotLockSQL, snapshotID).Error; err != nil {
			return err
		}
		var existing int64
		if err := tx.Raw(snapshotCountSQL, snapshotID, strings.TrimSpace(sq.WorkspaceID)).Scan(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			frozen = lead.Frozen{Size: int(existing), Existed: true}
			return nil
		}
		sql, args := s.freezeSQL(snapshotID, time.Now().UTC())
		result := tx.Exec(sql, args...)
		if result.Error != nil {
			return result.Error
		}
		frozen = lead.Frozen{Size: int(result.RowsAffected)}
		return nil
	})
	if err != nil {
		return lead.Frozen{}, fmt.Errorf("freeze lead selection %s: %w", snapshotID, err)
	}
	return frozen, nil
}

func (r *repository) SnapshotPage(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	snapshotID, err := snapshotRef(snapshotID)
	if err != nil {
		return nil, err
	}
	if after, err = keysetCursor(after); err != nil {
		return nil, err
	}
	sql := snapshotPageSQL
	args := []interface{}{snapshotID, workspaceID}
	if after != "" {
		sql += " AND lead_id > ?::uuid"
		args = append(args, after)
	}
	sql += " ORDER BY lead_id LIMIT ?"
	args = append(args, limit)
	ids := []string{}
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("read lead snapshot %s: %w", snapshotID, err)
	}
	return ids, nil
}

func (r *repository) SnapshotSize(ctx context.Context, workspaceID, snapshotID string) (int, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, lead.ErrLeadWorkspaceRequired
	}
	snapshotID, err := snapshotRef(snapshotID)
	if err != nil {
		return 0, err
	}
	var size int64
	if err := r.db.WithContext(ctx).Raw(snapshotCountSQL, snapshotID, workspaceID).Scan(&size).Error; err != nil {
		return 0, fmt.Errorf("size of lead snapshot %s: %w", snapshotID, err)
	}
	return int(size), nil
}

func (r *repository) DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	snapshotID, err := snapshotRef(snapshotID)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(snapshotDropSQL, snapshotID, workspaceID).Error
}

func (r *repository) DropSnapshotsBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(snapshotSweepSQL, before, limit)
	return result.RowsAffected, result.Error
}

func snapshotRef(id string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return "", fmt.Errorf("lead selection snapshot %q: %w", id, err)
	}
	return parsed.String(), nil
}

func keysetCursor(after string) (string, error) {
	after = strings.TrimSpace(after)
	if after == "" {
		return "", nil
	}
	parsed, err := uuid.Parse(after)
	if err != nil {
		return "", fmt.Errorf("lead selection cursor %q: %w", after, err)
	}
	return parsed.String(), nil
}

var (
	_ lead.SelectionReader    = (*repository)(nil)
	_ lead.SelectionSnapshots = (*repository)(nil)
)
