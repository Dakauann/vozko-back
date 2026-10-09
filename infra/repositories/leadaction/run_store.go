package leadaction_repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/leadaction"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	claimableRunSQL = "((status = 'queued' AND (not_before IS NULL OR not_before <= ?))" +
		" OR (status = 'running' AND " + database.LeaseFreeSQL + "))"

	claimSQL = "UPDATE lead_action_runs SET status = 'running', claim_token = ?, heartbeat_at = ?, not_before = NULL, attempts = attempts + 1," +
		" started_at = COALESCE(started_at, ?), updated_at = ?" +
		" WHERE id = ? AND attempts < ? AND " + claimableRunSQL + " RETURNING *"

	claimableSQL = "SELECT id::text FROM lead_action_runs WHERE attempts < ? AND " + claimableRunSQL + " ORDER BY created_at LIMIT ?"

	failStalledSQL = "WITH stalled AS (UPDATE lead_action_runs SET status = 'failed', failure_code = 'stalled', claim_token = NULL, finished_at = ?, updated_at = ?" +
		" WHERE attempts >= ? AND ((status = 'queued' AND (not_before IS NULL OR not_before <= ?))" +
		" OR (status = 'running' AND (heartbeat_at IS NULL OR heartbeat_at < ?))) RETURNING action)" +
		" SELECT action, count(*) AS runs FROM stalled GROUP BY action"

	saveSQL = "UPDATE lead_action_runs SET status = ?, phase = ?, cursor = ?, result = ?::jsonb, failure_code = ?, attempts = ?, claim_token = ?," +
		" heartbeat_at = ?, not_before = ?, started_at = ?, finished_at = ?, updated_at = ? WHERE id = ?"

	claimGuardSQL = " AND claim_token = ?"

	getSQL = "SELECT * FROM lead_action_runs WHERE workspace_id = ? AND id = ?"

	byKeySQL = "SELECT * FROM lead_action_runs WHERE workspace_id = ? AND idempotency_key = ?"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(ctx context.Context, r *leadaction.Run) error {
	row, err := rowOf(r)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return leadaction.ErrRunExists
		}
		return err
	}
	return nil
}

func (s *Store) FindByKey(ctx context.Context, workspaceID, key string) (*leadaction.Run, error) {
	workspaceID, key = strings.TrimSpace(workspaceID), strings.TrimSpace(key)
	if workspaceID == "" || key == "" {
		return nil, leadaction.ErrRunNotFound
	}
	return s.one(ctx, byKeySQL, workspaceID, key)
}

func (s *Store) Get(ctx context.Context, workspaceID, id string) (*leadaction.Run, error) {
	workspaceID, id = strings.TrimSpace(workspaceID), strings.TrimSpace(id)
	if workspaceID == "" || len(database.UUIDArray([]string{id})) == 0 {
		return nil, leadaction.ErrRunNotFound
	}
	return s.one(ctx, getSQL, workspaceID, id)
}

func (s *Store) one(ctx context.Context, sql string, args ...interface{}) (*leadaction.Run, error) {
	var rows []schema.LeadActionRun
	if err := s.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, leadaction.ErrRunNotFound
	}
	return runOf(rows[0])
}

func (s *Store) Claim(ctx context.Context, id, token string, now time.Time) (*leadaction.Run, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, leadaction.ErrRunNotFound
	}
	now = now.UTC()
	var rows []schema.LeadActionRun
	err := s.db.WithContext(ctx).Raw(claimSQL, token, now, now, now, id, leadaction.MaxAttempts, now, now.Add(-leadaction.StaleAfter)).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, leadaction.ErrClaimLost
	}
	return runOf(rows[0])
}

func (s *Store) Save(ctx context.Context, r *leadaction.Run, claim string) error {
	claim = strings.TrimSpace(claim)
	if claim == "" {
		return leadaction.ErrClaimLost
	}
	row, err := rowOf(r)
	if err != nil {
		return err
	}
	var ids []string
	err = s.db.WithContext(ctx).Raw(saveSQL+claimGuardSQL+" RETURNING id",
		row.Status, row.Phase, row.Cursor, string(row.Result), row.FailureCode, row.Attempts, row.ClaimToken,
		row.HeartbeatAt, row.NotBefore, row.StartedAt, row.FinishedAt, row.UpdatedAt, row.ID, claim,
	).Scan(&ids).Error
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return leadaction.ErrClaimLost
	}
	return nil
}

func (s *Store) Claimable(ctx context.Context, now time.Time, limit int) ([]string, error) {
	now = now.UTC()
	var ids []string
	err := s.db.WithContext(ctx).Raw(claimableSQL, leadaction.MaxAttempts, now, now.Add(-leadaction.StaleAfter), limit).Scan(&ids).Error
	return ids, err
}

type stalledRuns struct {
	Action string
	Runs   int
}

func (s *Store) FailStalled(ctx context.Context, now time.Time) (map[leadaction.Action]int, error) {
	now = now.UTC()
	var rows []stalledRuns
	if err := s.db.WithContext(ctx).Raw(failStalledSQL, now, now, leadaction.MaxAttempts, now, now.Add(-leadaction.StaleAfter)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	stalled := make(map[leadaction.Action]int, len(rows))
	for _, row := range rows {
		stalled[leadaction.Action(row.Action)] += row.Runs
	}
	return stalled, nil
}

func rowOf(r *leadaction.Run) (*schema.LeadActionRun, error) {
	params, err := json.Marshal(r.Params)
	if err != nil {
		return nil, fmt.Errorf("lead action params: %w", err)
	}
	result, err := json.Marshal(r.Result)
	if err != nil {
		return nil, fmt.Errorf("lead action result: %w", err)
	}
	return &schema.LeadActionRun{
		ID: r.ID, WorkspaceID: r.WorkspaceID, ActorID: r.ActorID, IsAdmin: r.IsAdmin, DepartmentID: schema.OptionalText(r.DepartmentID),
		Action: string(r.Action), Params: datatypes.JSON(params), IdempotencyKey: r.IdempotencyKey, RequestFingerprint: r.RequestFingerprint,
		Status: string(r.Status), Phase: string(r.Phase), Cursor: schema.OptionalText(r.Cursor), Result: datatypes.JSON(result),
		FailureCode: schema.OptionalText(r.FailureCode), Attempts: r.Attempts, ClaimToken: schema.OptionalText(r.Claim),
		HeartbeatAt: r.HeartbeatAt, NotBefore: r.NotBefore, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func runOf(row schema.LeadActionRun) (*leadaction.Run, error) {
	r := &leadaction.Run{
		ID: row.ID, WorkspaceID: row.WorkspaceID, ActorID: row.ActorID, IsAdmin: row.IsAdmin, DepartmentID: string(row.DepartmentID),
		Action: leadaction.Action(row.Action), IdempotencyKey: row.IdempotencyKey, RequestFingerprint: row.RequestFingerprint,
		Status: leadaction.Status(row.Status), Phase: leadaction.Phase(row.Phase), Cursor: string(row.Cursor),
		FailureCode: leadaction.FailureCode(row.FailureCode), Attempts: row.Attempts, Claim: string(row.ClaimToken),
		HeartbeatAt: row.HeartbeatAt, NotBefore: row.NotBefore, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal(row.Params, &r.Params); err != nil {
		return nil, fmt.Errorf("lead action run %s params: %w", row.ID, err)
	}
	if err := json.Unmarshal(row.Result, &r.Result); err != nil {
		return nil, fmt.Errorf("lead action run %s result: %w", row.ID, err)
	}
	if r.Result.Skipped == nil {
		r.Result.Skipped = map[leadaction.SkipReason]int{}
	}
	return r, nil
}

var _ leadaction.Store = (*Store)(nil)
