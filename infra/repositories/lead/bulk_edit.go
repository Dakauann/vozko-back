package lead

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/recordevent"
	"vozko/infra/database/schema"
)

const bulkTargetSQL = " FROM leads WHERE leads.workspace_id = ? AND leads.id = ANY(?::uuid[]) AND leads.deleted_at IS NULL"

var errBulkBatchTooLarge = fmt.Errorf("lead bulk edit: at most %d leads per batch", leadaction.BatchSize)

type bulkEdit struct {
	edit      leadaction.Edit
	valueJSON string
	ownerID   schema.OptionalText
	ownerKind schema.OptionalText
}

func newBulkEdit(e leadaction.Edit) (*bulkEdit, error) {
	b := &bulkEdit{edit: e}
	switch e.Kind {
	case leadaction.EditCustomField:
		if strings.TrimSpace(e.Key) == "" {
			return nil, leadaction.ErrKeyRequired
		}
		if e.Value != nil {
			raw, err := json.Marshal(e.Value)
			if err != nil {
				return nil, fmt.Errorf("lead bulk edit value: %w", err)
			}
			b.valueJSON = string(raw)
		}
	case leadaction.EditOwner:
		b.ownerID, b.ownerKind = ownerColumns(e.Owner())
	case leadaction.EditBlocked:
		if _, ok := e.Value.(bool); !ok {
			return nil, leadaction.ErrBlockedRequired
		}
	default:
		return nil, leadaction.ErrNotARun
	}
	return b, nil
}

func (b *bulkEdit) clears() bool {
	return b.edit.Kind == leadaction.EditCustomField && b.edit.Value == nil
}

func (b *bulkEdit) unchanged() (string, []interface{}) {
	switch b.edit.Kind {
	case leadaction.EditCustomField:
		if b.clears() {
			return "(leads.custom_fields -> ?::text) IS NULL", []interface{}{b.edit.Key}
		}
		return "(leads.custom_fields -> ?::text) IS NOT DISTINCT FROM ?::jsonb", []interface{}{b.edit.Key, b.valueJSON}
	case leadaction.EditOwner:
		if b.ownerID == "" {
			return "leads.owner_id IS NULL", nil
		}
		return "(leads.owner_id IS NOT DISTINCT FROM ?::uuid AND leads.owner_kind IS NOT DISTINCT FROM ?)", []interface{}{b.ownerID, b.ownerKind}
	default:
		return "leads.blocked = ?", []interface{}{b.edit.Blocks()}
	}
}

func (b *bulkEdit) set(actorID string, at time.Time) (string, []interface{}) {
	switch b.edit.Kind {
	case leadaction.EditCustomField:
		if b.clears() {
			return "custom_fields = NULLIF(COALESCE(leads.custom_fields, '{}'::jsonb) - ?::text, '{}'::jsonb)", []interface{}{b.edit.Key}
		}
		return "custom_fields = jsonb_set(COALESCE(leads.custom_fields, '{}'::jsonb), ARRAY[?::text], ?::jsonb, true)", []interface{}{b.edit.Key, b.valueJSON}
	case leadaction.EditOwner:
		return "owner_id = ?::uuid, owner_kind = ?", []interface{}{b.ownerID, b.ownerKind}
	default:
		if !b.edit.Blocks() {
			return "blocked = false, blocked_at = NULL, blocked_by = NULL", nil
		}
		return "blocked = true, blocked_at = ?, blocked_by = ?::uuid", []interface{}{at, schema.OptionalText(strings.TrimSpace(actorID))}
	}
}

func (b *bulkEdit) lockSQL(workspaceID string, ids []string) (string, []interface{}) {
	before, args := "NULL::jsonb", []interface{}{}
	if b.edit.Kind == leadaction.EditCustomField {
		before, args = "(leads.custom_fields -> ?::text)", []interface{}{b.edit.Key}
	}
	unchanged, unchangedArgs := b.unchanged()
	sql := "SELECT leads.id::text AS id, " + before + " AS before_value, leads.owner_id::text AS before_owner_id, leads.owner_kind AS before_owner_kind, (" +
		unchanged + ") AS unchanged" + bulkTargetSQL + " ORDER BY leads.id FOR UPDATE"
	args = append(append(args, unchangedArgs...), workspaceID, pq.StringArray(ids))
	return sql, args
}

func (b *bulkEdit) tallySQL(workspaceID string, ids []string) (string, []interface{}) {
	unchanged, args := b.unchanged()
	return "SELECT COUNT(*) AS existing, COUNT(*) FILTER (WHERE " + unchanged + ") AS unchanged" + bulkTargetSQL,
		append(append([]interface{}{}, args...), workspaceID, pq.StringArray(ids))
}

func (b *bulkEdit) updateSQL(workspaceID string, ids []string, actorID string, at time.Time) (string, []interface{}) {
	set, args := b.set(actorID, at)
	sql := "UPDATE leads SET " + set + ", version = leads.version + 1, updated_at = ?" +
		" WHERE leads.workspace_id = ? AND leads.id = ANY(?::uuid[]) AND leads.deleted_at IS NULL RETURNING leads.id::text AS id, leads.version"
	return sql, append(append([]interface{}{}, args...), at, workspaceID, pq.StringArray(ids))
}

type bulkLockedRow struct {
	ID              string
	BeforeValue     datatypes.JSON
	BeforeOwnerID   schema.OptionalText
	BeforeOwnerKind schema.OptionalText
	Unchanged       bool
}

func (b *bulkEdit) before(row bulkLockedRow) (any, error) {
	switch b.edit.Kind {
	case leadaction.EditCustomField:
		if len(row.BeforeValue) == 0 {
			return nil, nil
		}
		var value any
		if err := json.Unmarshal(row.BeforeValue, &value); err != nil {
			return nil, fmt.Errorf("lead %s field %s: %w", row.ID, b.edit.Key, err)
		}
		return value, nil
	case leadaction.EditOwner:
		return ownerOf(row.BeforeOwnerID, row.BeforeOwnerKind), nil
	}
	return nil, nil
}

type bulkChangedRow struct {
	ID      string
	Version int64
}

func bulkTargets(workspaceID string, ids []string) (string, []string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", nil, lead.ErrLeadWorkspaceRequired
	}
	if len(ids) > leadaction.BatchSize {
		return "", nil, errBulkBatchTooLarge
	}
	unique, err := lead.SelectionIDs(ids)
	if err != nil {
		return "", nil, err
	}
	return workspaceID, unique, nil
}

func (r *repository) ApplyBatch(ctx context.Context, w leadaction.BatchWrite) (leadaction.Tally, error) {
	workspaceID, ids, err := bulkTargets(w.WorkspaceID, w.LeadIDs)
	if err != nil {
		return leadaction.Tally{}, err
	}
	if strings.TrimSpace(w.ActorID) == "" {
		return leadaction.Tally{}, leadaction.ErrActorRequired
	}
	b, err := newBulkEdit(w.Edit)
	if err != nil {
		return leadaction.Tally{}, err
	}
	if len(ids) == 0 {
		return leadaction.Tally{}, nil
	}
	at := w.At.UTC()
	var tally leadaction.Tally
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tally = leadaction.Tally{}
		lockSQL, lockArgs := b.lockSQL(workspaceID, ids)
		var locked []bulkLockedRow
		if err := tx.Raw(lockSQL, lockArgs...).Scan(&locked).Error; err != nil {
			return err
		}
		tally.Gone = len(ids) - len(locked)
		before := make(map[string]bulkLockedRow, len(locked))
		changing := make([]string, 0, len(locked))
		for _, row := range locked {
			if row.Unchanged {
				tally.Unchanged++
				continue
			}
			before[row.ID] = row
			changing = append(changing, row.ID)
		}
		if len(changing) == 0 {
			return nil
		}
		updateSQL, updateArgs := b.updateSQL(workspaceID, changing, w.ActorID, at)
		var changed []bulkChangedRow
		if err := tx.Raw(updateSQL, updateArgs...).Scan(&changed).Error; err != nil {
			return err
		}
		events := make([]schema.LeadEvent, 0, len(changed))
		for _, row := range changed {
			was, err := b.before(before[row.ID])
			if err != nil {
				return err
			}
			rows, err := eventRows(workspaceID, row.ID, []recordevent.Event{w.Edit.Event(w.ActorID, was)}, at)
			if err != nil {
				return err
			}
			events = append(events, rows...)
			tally.Changed = append(tally.Changed, leadaction.Changed{LeadID: row.ID, Version: row.Version})
		}
		tally.Unchanged += len(changing) - len(changed)
		return insertEventRows(tx, events)
	})
	if err != nil {
		return leadaction.Tally{}, fmt.Errorf("lead bulk edit: %w", err)
	}
	if len(tally.Changed) > 0 {
		r.agg.bump(workspaceID)
	}
	return tally, nil
}

func (r *repository) TallyBatch(ctx context.Context, workspaceID string, ids []string, e leadaction.Edit) (leadaction.Tally, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return leadaction.Tally{}, lead.ErrLeadWorkspaceRequired
	}
	unique, err := lead.SelectionIDs(ids)
	if err != nil {
		return leadaction.Tally{}, err
	}
	b, err := newBulkEdit(e)
	if err != nil {
		return leadaction.Tally{}, err
	}
	if len(unique) == 0 {
		return leadaction.Tally{}, nil
	}
	sql, args := b.tallySQL(workspaceID, unique)
	var row struct {
		Existing  int
		Unchanged int
	}
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&row).Error; err != nil {
		return leadaction.Tally{}, fmt.Errorf("lead bulk tally: %w", err)
	}
	return leadaction.Tally{Unchanged: row.Unchanged, Gone: len(unique) - row.Existing}, nil
}

const blockTargetsSQL = "SELECT leads.id::text AS lead_id, leads.number" + bulkTargetSQL + " AND leads.blocked = ? AND leads.number IS NOT NULL ORDER BY leads.id"

func (r *repository) BlockTargets(ctx context.Context, workspaceID string, ids []string, blocked bool) ([]leadaction.BlockTarget, error) {
	workspaceID, unique, err := bulkTargets(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	if len(unique) == 0 {
		return []leadaction.BlockTarget{}, nil
	}
	targets := []leadaction.BlockTarget{}
	if err := r.db.WithContext(ctx).Raw(blockTargetsSQL, workspaceID, pq.StringArray(unique), blocked).Scan(&targets).Error; err != nil {
		return nil, fmt.Errorf("lead block targets: %w", err)
	}
	return targets, nil
}

var _ leadaction.BulkWriter = (*repository)(nil)
