package calllist_repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/calls/calllist"
	"vozko/domain/calls/cdr"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

var (
	itemsSelectSQL = "SELECT " + itemColumns + ", COALESCE(l.name, '') AS lead_name, COALESCE(l.number, '') AS lead_number," +
		" COALESCE(a.district, '') AS lead_district, COALESCE(a.city, '') AS lead_city," +
		" c.id AS call_id, c.call_id AS call_provider_id, c.status AS call_status, c.direction AS call_direction, c.source AS call_source," +
		" c.end_reason AS call_end_reason, c.answered_at AS call_answered_at, c.started_at AS call_started_at, c.ended_at AS call_ended_at," +
		" c.duration_sec AS call_duration_sec, c.agent_id::text AS call_agent_id, c.lead_id::text AS call_lead_id, c.deleted_at IS NOT NULL AS call_deleted," +
		" attempts.total AS attempts"

	itemsJoinSQL = " LEFT JOIN leads l ON l.id = i.lead_id AND l.workspace_id = i.workspace_id" +
		" LEFT JOIN lead_addresses a ON a.lead_id = i.lead_id AND a.workspace_id = i.workspace_id AND a.is_primary" +
		" LEFT JOIN calls c ON c.id = i.last_call_id AND c.workspace_id = i.workspace_id" +
		" LEFT JOIN LATERAL (SELECT COUNT(*) AS total FROM calls ca WHERE ca.workspace_id = i.workspace_id AND ca.lead_id = i.lead_id" +
		" AND ca.started_at >= i.created_at AND " + database.CallAttemptSQL("ca") + ") attempts ON true"

	itemsSQL = itemsSelectSQL + " FROM call_list_items i" + itemsJoinSQL + " WHERE i.workspace_id = ? AND i.list_id = ?"

	agendaKeySQL = database.CallListAgendaSQL("")

	agendaDueSQL = agendaSegmentSQL(calllist.SegmentDue) + " AND " + agendaKeySQL + " <= ?::timestamptz"

	agendaQueueSQL = agendaSegmentSQL(calllist.SegmentQueue) + " AND " + agendaKeySQL + " = 'infinity'::timestamptz"

	agendaWaitingSQL = agendaSegmentSQL(calllist.SegmentWaiting) + " AND " + agendaKeySQL + " > ?::timestamptz AND " + agendaKeySQL + " < 'infinity'::timestamptz"

	agendaTimeAfterSQL = " AND (" + agendaKeySQL + ", position) > (?::timestamptz, ?)"

	agendaBranchOrderSQL = " ORDER BY " + agendaKeySQL + ", position LIMIT ?"

	agendaPageSQL = itemsSelectSQL + " FROM (SELECT id, segment, agenda_at, position FROM (%s) segments ORDER BY segment, agenda_at, position LIMIT ?) agenda" +
		" JOIN call_list_items i ON i.id = agenda.id" + itemsJoinSQL +
		" ORDER BY agenda.segment, agenda.agenda_at, agenda.position"
)

func agendaSegmentSQL(segment calllist.AgendaSegment) string {
	return fmt.Sprintf("SELECT id, %d AS segment, %s AS agenda_at, position FROM call_list_items"+
		" WHERE workspace_id = ? AND list_id = ? AND state = 'pending'", int(segment), agendaKeySQL)
}

const (
	getListSQL = "SELECT * FROM call_lists WHERE workspace_id = ? AND id = ?"

	pageFilterSQL = " WHERE workspace_id = ?"

	assignedSQL = " AND ?::uuid = ANY(assignee_ids)"

	statusSQL = " AND status = ?"

	pageOrderSQL = " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"

	updateListSQL = "UPDATE call_lists SET name = ?, assignee_ids = ?::uuid[], status = ?, updated_at = ?" +
		" WHERE workspace_id = ? AND id = ? AND status NOT IN ('building', 'failed')"

	deleteListSQL = "DELETE FROM call_lists WHERE workspace_id = ? AND id = ?"

	buildClaimableSQL = "status = 'building' AND attempts < ? AND " + database.LeaseFreeSQL

	claimBuildSQL = "UPDATE call_lists SET claim_token = ?, heartbeat_at = ?, attempts = attempts + 1, updated_at = ?" +
		" WHERE workspace_id = ? AND id = ? AND " + buildClaimableSQL + " RETURNING *"

	lockBuildSQL = "SELECT * FROM call_lists WHERE workspace_id = ? AND id = ? AND status = 'building' AND claim_token = ? FOR UPDATE"

	appendItemsSQL = "INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, created_at, updated_at)" +
		" SELECT f.id, ?::uuid, ?::uuid, f.lead_id, f.phone," +
		" (SELECT COALESCE(MAX(e.position), 0) FROM call_list_items e WHERE e.list_id = ?::uuid) + row_number() OVER (ORDER BY f.ord), 'pending', ?, ?" +
		" FROM (SELECT DISTINCT ON (b.lead_id) b.id, b.lead_id, b.phone, b.ord" +
		" FROM unnest(?::uuid[], ?::uuid[], ?::text[]) WITH ORDINALITY AS b(id, lead_id, phone, ord)" +
		" WHERE NOT EXISTS (SELECT 1 FROM call_list_items e WHERE e.list_id = ?::uuid AND e.lead_id = b.lead_id)" +
		" ORDER BY b.lead_id, b.ord) f" +
		" ON CONFLICT (list_id, lead_id) DO NOTHING"

	advanceBuildSQL = "UPDATE call_lists SET item_count = item_count + ?, skipped = ?::jsonb, build_cursor = ?, heartbeat_at = ?, updated_at = ?" +
		" WHERE workspace_id = ? AND id = ?"

	finishBuildSQL = "UPDATE call_lists SET status = ?, failure_code = ?, claim_token = NULL, heartbeat_at = NULL, built_at = ?, updated_at = ?" +
		" WHERE workspace_id = ? AND id = ? AND status = 'building' AND claim_token = ?"

	buildableSQL = "SELECT workspace_id::text AS workspace_id, id::text AS id FROM call_lists WHERE " + buildClaimableSQL + " ORDER BY created_at LIMIT ?"

	failExhaustedSQL = "UPDATE call_lists SET status = 'failed', failure_code = ?, claim_token = NULL, heartbeat_at = NULL, updated_at = ?" +
		" WHERE status = 'building' AND attempts >= ? AND " + database.LeaseFreeSQL

	itemColumns = "i.id, i.workspace_id, i.list_id, i.lead_id, i.phone, i.position, i.state, i.reserved_by, i.reserved_until, i.disposition, i.note," +
		" i.refusal, i.callback_at, i.last_call_id, i.closed_by, i.closed_at, i.created_at, i.updated_at"

	itemsAfterSQL = " AND i.position > ?"

	itemStateSQL = " AND i.state = ?"

	itemsOrderSQL = " ORDER BY i.position LIMIT ?"

	agendaPositionAfterSQL = " AND position > ?"

	getItemSQL = "SELECT * FROM call_list_items WHERE workspace_id = ? AND id = ?"

	lockItemSQL = getItemSQL + " FOR UPDATE"

	workerLockSQL = "SELECT pg_advisory_xact_lock(hashtextextended(?::text, 0))"

	releaseExpiredSQL = "UPDATE call_list_items SET state = 'pending', reserved_by = NULL, reserved_until = NULL, updated_at = ?" +
		" WHERE workspace_id = ? AND reserved_by = ? AND state = 'reserved' AND (reserved_until IS NULL OR reserved_until <= ?)"

	heldSQL = "SELECT * FROM call_list_items WHERE workspace_id = ? AND reserved_by = ? AND state = 'reserved' LIMIT 1 FOR UPDATE"

	dueCallbackSQL = "SELECT * FROM call_list_items WHERE workspace_id = ? AND list_id = ? AND state = 'pending'" +
		" AND callback_at IS NOT NULL AND callback_at <= ? ORDER BY callback_at, position LIMIT 1 FOR UPDATE SKIP LOCKED"

	expiredSQL = "SELECT * FROM call_list_items WHERE workspace_id = ? AND list_id = ? AND state = 'reserved'" +
		" AND (reserved_until IS NULL OR reserved_until <= ?) ORDER BY position LIMIT 1 FOR UPDATE SKIP LOCKED"

	queuedSQL = "SELECT * FROM call_list_items WHERE workspace_id = ? AND list_id = ? AND state = 'pending'" +
		" AND callback_at IS NULL ORDER BY position LIMIT 1 FOR UPDATE SKIP LOCKED"

	writeItemSQL = "UPDATE call_list_items SET state = ?, reserved_by = ?, reserved_until = ?, disposition = ?, note = ?, refusal = ?," +
		" callback_at = ?, last_call_id = ?, closed_by = ?, closed_at = ?, updated_at = ? WHERE workspace_id = ? AND id = ?"

	progressSQL = "UPDATE call_lists SET closed_count = closed_count + ?, called_count = GREATEST(0, called_count + ?)," +
		" callback_count = GREATEST(0, callback_count + ?), updated_at = ?" +
		" WHERE workspace_id = ? AND id = ?"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

var _ calllist.Store = (*Store)(nil)

func validRef(workspaceID, id string) bool {
	return strings.TrimSpace(workspaceID) != "" && len(database.UUIDArray([]string{id})) == 1
}

func (s *Store) Create(ctx context.Context, l *calllist.List) (*calllist.List, bool, error) {
	if !validRef(l.WorkspaceID, l.ID) {
		return nil, false, calllist.ErrListNotFound
	}
	row, err := listRow(l)
	if err != nil {
		return nil, false, err
	}
	var created *calllist.List
	existed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec("INSERT INTO call_lists (id, workspace_id, name, created_by, assignee_ids, status, phone_source, phone_label,"+
			" selected, item_count, closed_count, skipped, attempts, created_at, updated_at)"+
			" VALUES (?, ?, ?, ?, ?::uuid[], ?, ?, ?, ?, 0, 0, ?::jsonb, 0, ?, ?) ON CONFLICT (id) DO NOTHING",
			row.ID, row.WorkspaceID, row.Name, row.CreatedBy, row.AssigneeIDs, row.Status, row.PhoneSource, row.PhoneLabel,
			row.Selected, string(row.Skipped), row.CreatedAt, row.UpdatedAt)
		if result.Error != nil {
			return result.Error
		}
		existed = result.RowsAffected == 0
		created, err = oneList(tx, getListSQL, row.WorkspaceID, row.ID)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("create call list %s: %w", l.ID, err)
	}
	return created, existed, nil
}

func (s *Store) Get(ctx context.Context, workspaceID, id string) (*calllist.List, error) {
	if !validRef(workspaceID, id) {
		return nil, calllist.ErrListNotFound
	}
	return oneList(s.db.WithContext(ctx), getListSQL, workspaceID, id)
}

func oneList(db *gorm.DB, sql string, args ...interface{}) (*calllist.List, error) {
	var rows []schema.CallList
	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, calllist.ErrListNotFound
	}
	return listOf(rows[0])
}

func pageWhere(q calllist.ListQuery) (string, []interface{}) {
	where, args := pageFilterSQL, []interface{}{q.WorkspaceID}
	if !q.Manages {
		where += assignedSQL
		args = append(args, q.ViewerID)
	}
	if q.Status != "" {
		where += statusSQL
		args = append(args, string(q.Status))
	}
	return where, args
}

func (s *Store) Page(ctx context.Context, q calllist.ListQuery) (calllist.ListPage, error) {
	q = q.Normalized()
	if strings.TrimSpace(q.WorkspaceID) == "" {
		return calllist.ListPage{}, calllist.ErrWorkspaceRequired
	}
	if !q.Manages && len(database.UUIDArray([]string{q.ViewerID})) == 0 {
		return calllist.ListPage{Lists: []*calllist.List{}}, nil
	}
	where, args := pageWhere(q)
	db := s.db.WithContext(ctx)
	var total int64
	if err := db.Raw("SELECT COUNT(*) FROM call_lists"+where, args...).Scan(&total).Error; err != nil {
		return calllist.ListPage{}, fmt.Errorf("count call lists: %w", err)
	}
	var rows []schema.CallList
	pageArgs := append(append([]interface{}{}, args...), q.PageSize, (q.Page-1)*q.PageSize)
	if err := db.Raw("SELECT * FROM call_lists"+where+pageOrderSQL, pageArgs...).Scan(&rows).Error; err != nil {
		return calllist.ListPage{}, fmt.Errorf("page call lists: %w", err)
	}
	lists := make([]*calllist.List, 0, len(rows))
	for _, row := range rows {
		l, err := listOf(row)
		if err != nil {
			return calllist.ListPage{}, err
		}
		lists = append(lists, l)
	}
	return calllist.ListPage{Lists: lists, Total: total}, nil
}

func (s *Store) Update(ctx context.Context, l *calllist.List) error {
	if !validRef(l.WorkspaceID, l.ID) {
		return calllist.ErrListNotFound
	}
	result := s.db.WithContext(ctx).Exec(updateListSQL, l.Name, database.UUIDArray(l.AssigneeIDs), string(l.Status),
		l.UpdatedAt.UTC(), l.WorkspaceID, l.ID)
	if result.Error != nil {
		return fmt.Errorf("update call list %s: %w", l.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return calllist.ErrListNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, workspaceID, id string) error {
	if !validRef(workspaceID, id) {
		return calllist.ErrListNotFound
	}
	result := s.db.WithContext(ctx).Exec(deleteListSQL, workspaceID, id)
	if result.Error != nil {
		return fmt.Errorf("delete call list %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return calllist.ErrListNotFound
	}
	return nil
}

func (s *Store) ClaimBuild(ctx context.Context, workspaceID, id, token string, now time.Time) (*calllist.List, error) {
	if !validRef(workspaceID, id) || strings.TrimSpace(token) == "" {
		return nil, calllist.ErrBuildClaimLost
	}
	now = now.UTC()
	l, err := oneList(s.db.WithContext(ctx), claimBuildSQL, token, now, now, workspaceID, id, calllist.MaxBuildAttempts, now.Add(-calllist.BuildLeaseStale))
	if err == calllist.ErrListNotFound {
		return nil, calllist.ErrBuildClaimLost
	}
	return l, err
}

func (s *Store) AppendItems(ctx context.Context, b calllist.BuildBatch) error {
	if !validRef(b.WorkspaceID, b.ListID) || strings.TrimSpace(b.Claim) == "" {
		return calllist.ErrBuildClaimLost
	}
	if b.Cursor != "" && len(database.UUIDArray([]string{b.Cursor})) == 0 {
		return calllist.ErrBuildCursorInvalid
	}
	at := b.At.UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		l, err := oneList(tx, lockBuildSQL, b.WorkspaceID, b.ListID, b.Claim)
		if err == calllist.ErrListNotFound {
			return calllist.ErrBuildClaimLost
		}
		if err != nil {
			return err
		}
		inserted := int64(0)
		if len(b.Items) > 0 {
			ids, leads, phones := make(pq.StringArray, 0, len(b.Items)), make(pq.StringArray, 0, len(b.Items)), make(pq.StringArray, 0, len(b.Items))
			for _, item := range b.Items {
				ids = append(ids, uuid.NewString())
				leads = append(leads, item.LeadID)
				phones = append(phones, item.Phone)
			}
			result := tx.Exec(appendItemsSQL, b.WorkspaceID, b.ListID, b.ListID, at, at, ids, leads, phones, b.ListID)
			if result.Error != nil {
				return fmt.Errorf("append call list items: %w", result.Error)
			}
			inserted = result.RowsAffected
		}
		skipped := calllist.Skips{}
		skipped.Merge(l.Skipped)
		skipped.Merge(b.Skipped)
		raw, err := json.Marshal(skipped)
		if err != nil {
			return err
		}
		return tx.Exec(advanceBuildSQL, inserted, string(raw), schema.OptionalText(b.Cursor), at, at, b.WorkspaceID, b.ListID).Error
	})
}

func (s *Store) FinishBuild(ctx context.Context, workspaceID, id, token string, status calllist.Status, failureCode string, now time.Time) error {
	if !validRef(workspaceID, id) || strings.TrimSpace(token) == "" {
		return calllist.ErrBuildClaimLost
	}
	now = now.UTC()
	result := s.db.WithContext(ctx).Exec(finishBuildSQL, string(status), schema.OptionalText(failureCode), now, now, workspaceID, id, token)
	if result.Error != nil {
		return fmt.Errorf("finish call list %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return calllist.ErrBuildClaimLost
	}
	return nil
}

func (s *Store) Buildable(ctx context.Context, now time.Time, limit int) ([]calllist.BuildRef, error) {
	now = now.UTC()
	refs := []calllist.BuildRef{}
	err := s.db.WithContext(ctx).Raw(buildableSQL, calllist.MaxBuildAttempts, now.Add(-calllist.BuildLeaseStale), limit).Scan(&refs).Error
	return refs, err
}

func (s *Store) FailExhausted(ctx context.Context, now time.Time) (int64, error) {
	now = now.UTC()
	result := s.db.WithContext(ctx).Exec(failExhaustedSQL, calllist.FailureBuild, now, calllist.MaxBuildAttempts, now.Add(-calllist.BuildLeaseStale))
	return result.RowsAffected, result.Error
}

type itemViewRow struct {
	schema.CallListItem
	LeadName        string
	LeadNumber      string
	CallID          *string
	CallProviderID  *string
	CallStatus      *string
	CallDirection   *string
	CallSource      *string
	CallEndReason   *string
	CallAnsweredAt  *time.Time
	CallStartedAt   *time.Time
	CallEndedAt     *time.Time
	CallDurationSec *int
	CallAgentID     *string
	CallLeadID      *string
	CallDeleted     bool
	LeadDistrict    string
	LeadCity        string
	Attempts        int
}

func agendaBranchOf(q calllist.ItemQuery, segment calllist.AgendaSegment, limit int) (string, []interface{}) {
	cursor := q.Continues() && segment == q.CursorSegment()
	args := []interface{}{q.WorkspaceID, q.ListID}
	var sql string
	switch segment {
	case calllist.SegmentDue:
		sql = agendaDueSQL
		args = append(args, q.AsOf)
	case calllist.SegmentQueue:
		sql = agendaQueueSQL
	default:
		sql = agendaWaitingSQL
		args = append(args, q.AsOf)
	}
	switch {
	case cursor && segment == calllist.SegmentQueue:
		sql += agendaPositionAfterSQL
		args = append(args, q.AfterPosition)
	case cursor:
		sql += agendaTimeAfterSQL
		args = append(args, *q.AfterAt, q.AfterPosition)
	}
	return "(" + sql + agendaBranchOrderSQL + ")", append(args, limit)
}

func agendaPageOf(q calllist.ItemQuery) (string, []interface{}) {
	limit := q.Limit + 1
	branches, args := []string{}, []interface{}{}
	for _, segment := range []calllist.AgendaSegment{calllist.SegmentDue, calllist.SegmentQueue, calllist.SegmentWaiting} {
		if segment < q.CursorSegment() {
			continue
		}
		sql, branchArgs := agendaBranchOf(q, segment, limit)
		branches = append(branches, sql)
		args = append(args, branchArgs...)
	}
	return fmt.Sprintf(agendaPageSQL, strings.Join(branches, " UNION ALL ")), append(args, limit)
}

func itemsPageSQL(q calllist.ItemQuery) (string, []interface{}) {
	if q.Agenda() {
		return agendaPageOf(q)
	}
	sql, args := itemsSQL, []interface{}{q.WorkspaceID, q.ListID}
	sql += itemsAfterSQL
	args = append(args, q.AfterPosition)
	if q.State != "" {
		sql += itemStateSQL
		args = append(args, string(q.State))
	}
	return sql + itemsOrderSQL, append(args, q.Limit+1)
}

func (s *Store) Items(ctx context.Context, q calllist.ItemQuery) (calllist.ItemPage, error) {
	q = q.Normalized()
	if !validRef(q.WorkspaceID, q.ListID) {
		return calllist.ItemPage{}, calllist.ErrListNotFound
	}
	if err := q.Check(); err != nil {
		return calllist.ItemPage{}, err
	}
	q.AsOf = q.AsOf.Truncate(time.Microsecond)
	sql, args := itemsPageSQL(q)
	var rows []itemViewRow
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return calllist.ItemPage{}, fmt.Errorf("call list items: %w", err)
	}
	page := calllist.ItemPage{Items: make([]calllist.ItemView, 0, len(rows))}
	if q.Agenda() {
		asOf := q.AsOf
		page.AsOf = &asOf
	}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		page.Next = last.Position
		if q.Agenda() && last.CallbackAt != nil {
			at := last.CallbackAt.UTC()
			page.NextAt = &at
		}
	}
	for _, row := range rows {
		page.Items = append(page.Items, viewOf(row))
	}
	return page, nil
}

func viewOf(row itemViewRow) calllist.ItemView {
	view := calllist.ItemView{Item: itemOf(row.CallListItem), LeadName: row.LeadName, LeadNumber: row.LeadNumber,
		LeadDistrict: row.LeadDistrict, LeadCity: row.LeadCity, Attempts: row.Attempts}
	if row.CallID != nil {
		view.LastCall = callOf(row)
	}
	if row.CallID != nil && !row.CallDeleted {
		view.StampedCall = calllist.FactsOf(view.LastCall)
	}
	return view
}

func (s *Store) Item(ctx context.Context, workspaceID, id string) (*calllist.Item, error) {
	if !validRef(workspaceID, id) {
		return nil, calllist.ErrItemNotFound
	}
	return oneItem(s.db.WithContext(ctx), getItemSQL, workspaceID, id)
}

func oneItem(db *gorm.DB, sql string, args ...interface{}) (*calllist.Item, error) {
	var rows []schema.CallListItem
	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, calllist.ErrItemNotFound
	}
	item := itemOf(rows[0])
	return &item, nil
}

func workerKey(workspaceID, userID string) string {
	return "call_list_worker:" + workspaceID + ":" + userID
}

func (s *Store) Next(ctx context.Context, c calllist.NextClaim) (*calllist.Item, error) {
	if !validRef(c.WorkspaceID, c.ListID) {
		return nil, calllist.ErrListNotFound
	}
	if len(database.UUIDArray([]string{c.UserID})) == 0 {
		return nil, calllist.ErrActorRequired
	}
	now := c.Now.UTC()
	var claimed *calllist.Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(workerLockSQL, workerKey(c.WorkspaceID, c.UserID)).Error; err != nil {
			return err
		}
		if err := tx.Exec(releaseExpiredSQL, now, c.WorkspaceID, c.UserID, now).Error; err != nil {
			return fmt.Errorf("release expired call list reservations: %w", err)
		}
		held, err := oneItem(tx, heldSQL, c.WorkspaceID, c.UserID)
		switch {
		case err == nil && held.ListID != c.ListID:
			return calllist.ErrReservationHeld
		case err == nil:
			claimed = held
			return reserve(tx, held, c.UserID, now)
		case err != calllist.ErrItemNotFound:
			return err
		}
		for _, candidate := range []struct {
			sql  string
			args []interface{}
		}{
			{dueCallbackSQL, []interface{}{c.WorkspaceID, c.ListID, now}},
			{expiredSQL, []interface{}{c.WorkspaceID, c.ListID, now}},
			{queuedSQL, []interface{}{c.WorkspaceID, c.ListID}},
		} {
			item, err := oneItem(tx, candidate.sql, candidate.args...)
			if err == calllist.ErrItemNotFound {
				continue
			}
			if err != nil {
				return err
			}
			claimed = item
			return reserve(tx, item, c.UserID, now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func reserve(tx *gorm.DB, item *calllist.Item, userID string, now time.Time) error {
	if err := item.Reserve(userID, now); err != nil {
		return err
	}
	return writeItem(tx, item)
}

func writeItem(tx *gorm.DB, item *calllist.Item) error {
	row := itemRow(*item)
	result := tx.Exec(writeItemSQL, row.State, row.ReservedBy, row.ReservedUntil, row.Disposition, row.Note, row.Refusal,
		row.CallbackAt, row.LastCallID, row.ClosedBy, row.ClosedAt, row.UpdatedAt, row.WorkspaceID, row.ID)
	if result.Error != nil {
		return fmt.Errorf("write call list item %s: %w", item.ID, result.Error)
	}
	if result.RowsAffected == 0 {
		return calllist.ErrItemNotFound
	}
	return nil
}

func (s *Store) Mutate(ctx context.Context, workspaceID, itemID string, fn calllist.Mutation) (*calllist.Item, error) {
	if !validRef(workspaceID, itemID) {
		return nil, calllist.ErrItemNotFound
	}
	var out *calllist.Item
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := oneItem(tx, lockItemSQL, workspaceID, itemID)
		if err != nil {
			return err
		}
		l, err := oneList(tx, getListSQL, workspaceID, item.ListID)
		if err != nil {
			return err
		}
		before := *item
		if err := fn(item, l); err != nil {
			return err
		}
		if err := writeItem(tx, item); err != nil {
			return err
		}
		if change := calllist.ProgressChange(before, *item); !change.None() {
			if err := tx.Exec(progressSQL, change.Closed, change.Called, change.Callbacks, item.UpdatedAt.UTC(), workspaceID, item.ListID).Error; err != nil {
				return fmt.Errorf("count the progress of call list %s: %w", item.ListID, err)
			}
		}
		out = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func listRow(l *calllist.List) (*schema.CallList, error) {
	skipped := l.Skipped
	if skipped == nil {
		skipped = calllist.Skips{}
	}
	raw, err := json.Marshal(skipped)
	if err != nil {
		return nil, fmt.Errorf("call list skips: %w", err)
	}
	return &schema.CallList{
		ID: l.ID, WorkspaceID: l.WorkspaceID, Name: l.Name, CreatedBy: l.CreatedBy, AssigneeIDs: database.UUIDArray(l.AssigneeIDs),
		Status: string(l.Status), PhoneSource: string(l.Phone.Source), PhoneLabel: schema.OptionalText(l.Phone.Label),
		Selected:  l.Selected,
		ItemCount: l.ItemCount, ClosedCount: l.ClosedCount, Skipped: datatypes.JSON(raw), FailureCode: schema.OptionalText(l.FailureCode),
		CreatedAt: l.CreatedAt.UTC(), UpdatedAt: l.UpdatedAt.UTC(),
	}, nil
}

func listOf(row schema.CallList) (*calllist.List, error) {
	skipped := calllist.Skips{}
	if len(row.Skipped) > 0 {
		if err := json.Unmarshal(row.Skipped, &skipped); err != nil {
			return nil, fmt.Errorf("call list %s skips: %w", row.ID, err)
		}
	}
	assignees := []string(row.AssigneeIDs)
	if assignees == nil {
		assignees = []string{}
	}
	return &calllist.List{
		ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, CreatedBy: row.CreatedBy, AssigneeIDs: assignees,
		Status: calllist.Status(row.Status), Phone: calllist.PhoneChoice{Source: calllist.PhoneSource(row.PhoneSource), Label: lead.PhoneLabel(row.PhoneLabel)},
		Selected: row.Selected, ItemCount: row.ItemCount,
		ClosedCount: row.ClosedCount, CalledCount: row.CalledCount, CallbackCount: row.CallbackCount, Skipped: skipped, FailureCode: string(row.FailureCode),
		Build:       shared.Lease{Claim: string(row.ClaimToken), HeartbeatAt: row.HeartbeatAt, Attempts: row.Attempts},
		BuildCursor: string(row.BuildCursor), BuiltAt: row.BuiltAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

func itemRow(i calllist.Item) schema.CallListItem {
	return schema.CallListItem{
		ID: i.ID, WorkspaceID: i.WorkspaceID, ListID: i.ListID, LeadID: i.LeadID, Phone: i.Phone, Position: i.Position,
		State: string(i.State), ReservedBy: schema.OptionalText(i.ReservedBy), ReservedUntil: utcPtr(i.ReservedUntil),
		Disposition: schema.OptionalText(i.Disposition), Note: schema.OptionalText(i.Note), Refusal: schema.OptionalText(i.Refusal),
		CallbackAt: utcPtr(i.CallbackAt), LastCallID: schema.OptionalText(i.LastCallID), ClosedBy: schema.OptionalText(i.ClosedBy),
		ClosedAt: utcPtr(i.ClosedAt), CreatedAt: i.CreatedAt.UTC(), UpdatedAt: i.UpdatedAt.UTC(),
	}
}

func itemOf(row schema.CallListItem) calllist.Item {
	return calllist.Item{
		ID: row.ID, WorkspaceID: row.WorkspaceID, ListID: row.ListID, LeadID: row.LeadID, Phone: row.Phone, Position: row.Position,
		State: calllist.State(row.State), ReservedBy: string(row.ReservedBy), ReservedUntil: row.ReservedUntil,
		Disposition: string(row.Disposition), Note: string(row.Note), Refusal: string(row.Refusal), CallbackAt: row.CallbackAt,
		LastCallID: string(row.LastCallID), ClosedBy: string(row.ClosedBy), ClosedAt: row.ClosedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func callOf(row itemViewRow) *cdr.Call {
	call := &cdr.Call{ID: deref(row.CallID), CallID: deref(row.CallProviderID), WorkspaceID: row.WorkspaceID,
		Status: cdr.Status(deref(row.CallStatus)), Direction: cdr.Direction(deref(row.CallDirection)), Source: cdr.Source(deref(row.CallSource)),
		EndReason: row.CallEndReason, AnsweredAt: row.CallAnsweredAt, EndedAt: row.CallEndedAt, AgentID: row.CallAgentID, LeadID: row.CallLeadID}
	if row.CallStartedAt != nil {
		call.StartedAt = *row.CallStartedAt
	}
	if row.CallDurationSec != nil {
		call.DurationSec = *row.CallDurationSec
	}
	return call
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
