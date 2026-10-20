package workflow_repository

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

const runtimeAlertColumns = `id, workspace_id, workflow_id, node_id, agent_id, subject, kind, count, first_seen, last_seen, sample, status, resolved_at, notified_at`

const recordRuntimeAlertSQL = `INSERT INTO workflow_runtime_alerts (id, workspace_id, workflow_id, node_id, agent_id, subject, kind, count, first_seen, last_seen, sample, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', now(), now())
ON CONFLICT (workspace_id, workflow_id, node_id, agent_id, subject, kind) WHERE status = 'open'
DO UPDATE SET count = workflow_runtime_alerts.count + EXCLUDED.count,
	last_seen = GREATEST(workflow_runtime_alerts.last_seen, EXCLUDED.last_seen),
	sample = EXCLUDED.sample,
	updated_at = now()`

const openRuntimeAlertsPrefix = `INSERT INTO workflow_runtime_alerts (id, workspace_id, workflow_id, node_id, agent_id, subject, kind, count, first_seen, last_seen, sample, status, created_at, updated_at)
VALUES `

const openRuntimeAlertRow = `(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', now(), now())`

const openRuntimeAlertsSuffix = `
ON CONFLICT (workspace_id, workflow_id, node_id, agent_id, subject, kind) WHERE status = 'open' DO NOTHING`

const touchRuntimeAlertsPrefix = `UPDATE workflow_runtime_alerts a
SET count = v.count, last_seen = v.last_seen, sample = v.sample, updated_at = now()
FROM (VALUES `

const touchRuntimeAlertRow = `(CAST(? AS uuid), CAST(? AS integer), CAST(? AS timestamptz), CAST(? AS varchar))`

const touchRuntimeAlertsSuffix = `) AS v(id, count, last_seen, sample)
WHERE a.id = v.id AND a.status = 'open'`

const resolveRuntimeAlertsSQL = `UPDATE workflow_runtime_alerts SET status = 'resolved', resolved_at = ?, updated_at = now()
WHERE id IN ? AND status = 'open'`

const markRuntimeAlertsNotifiedSQL = `UPDATE workflow_runtime_alerts SET notified_at = ?, updated_at = now()
WHERE id IN ? AND status = 'open'`

const trackedRuntimeAlertsSQL = `SELECT ` + runtimeAlertColumns + ` FROM workflow_runtime_alerts
WHERE status = 'open' OR (status = 'resolved' AND resolved_at >= ?)
ORDER BY status, last_seen DESC
LIMIT ?`

const workflowRuntimeAlertsFrom = `SELECT ` + runtimeAlertColumns + ` FROM workflow_runtime_alerts
WHERE workspace_id = ? AND workflow_id = ? AND (status = 'open' OR (status = 'resolved' AND resolved_at >= ?))`

const workflowRuntimeAlertsOrder = `
ORDER BY status, last_seen DESC
LIMIT ?`

const workflowRuntimeAlertsSQL = workflowRuntimeAlertsFrom + workflowRuntimeAlertsOrder

const openRuntimeAlertsFrom = ` FROM workflow_runtime_alerts a
WHERE a.workspace_id = ? AND a.status = 'open'`

const openRuntimeAlertsInDepartments = `
  AND EXISTS (SELECT 1 FROM workflows w WHERE w.id = CAST(NULLIF(a.workflow_id, '') AS uuid) AND w.department_id IN ?)`

const deleteResolvedRuntimeAlertsSQL = `DELETE FROM workflow_runtime_alerts
WHERE id IN (
	SELECT id FROM workflow_runtime_alerts
	WHERE status = 'resolved' AND resolved_at < ?
	LIMIT ?
)`

const rejectedDeliveriesSQL = `SELECT workspace_id::text AS workspace_id, workflow_id::text AS workflow_id, reason,
	COUNT(*) AS count, MIN(received_at) AS first_seen, MAX(received_at) AS last_seen,
	(ARRAY_AGG(id::text ORDER BY received_at DESC))[1] AS sample
FROM workflow_webhook_deliveries
WHERE received_at >= ? AND outcome = 'rejected'
GROUP BY workspace_id, workflow_id, reason
ORDER BY last_seen DESC LIMIT ?`

const failedToolCallsSQL = `SELECT workspace_id::text AS workspace_id, agent_id, tool_name, COALESCE(error_code, '') AS error_code,
	COUNT(*) AS count, MIN(created_at) AS first_seen, MAX(created_at) AS last_seen,
	(ARRAY_AGG(id::text ORDER BY created_at DESC))[1] AS sample
FROM ai_tool_calls
WHERE created_at >= ? AND outcome = 'error'
  AND (tool_name = 'http_request' OR tool_name LIKE 'run\_workflow\_%')
GROUP BY workspace_id, agent_id, tool_name, COALESCE(error_code, '')
ORDER BY last_seen DESC LIMIT ?`

const failingNodesSQL = `WITH failing AS (
	SELECT DISTINCT r.workflow_id, l.node_id
	FROM workflow_run_logs l
	JOIN workflow_runs r ON r.id = l.run_id
	WHERE l.executed_at >= ? AND l.status = 'failed'
), recent AS (
	SELECT r.workspace_id, r.workflow_id, l.node_id, l.status, l.run_id, l.executed_at,
		ROW_NUMBER() OVER (PARTITION BY r.workflow_id, l.node_id ORDER BY l.executed_at DESC) AS position
	FROM failing f
	JOIN workflow_runs r ON r.workflow_id = f.workflow_id AND r.updated_at >= ?
	JOIN workflow_run_logs l ON l.run_id = r.id AND l.node_id = f.node_id
	WHERE l.executed_at >= ? AND l.status IN ('executed', 'failed')
)
SELECT workspace_id::text AS workspace_id, workflow_id::text AS workflow_id, node_id,
	COUNT(*) FILTER (WHERE status = 'failed') AS count,
	MIN(executed_at) FILTER (WHERE status = 'failed') AS first_seen,
	MAX(executed_at) FILTER (WHERE status = 'failed') AS last_seen,
	(ARRAY_AGG(run_id::text ORDER BY executed_at DESC) FILTER (WHERE status = 'failed'))[1] AS sample
FROM recent
WHERE position <= ?
GROUP BY workspace_id, workflow_id, node_id
HAVING COUNT(*) FILTER (WHERE status = 'failed') > 0
ORDER BY last_seen DESC LIMIT ?`

const nodeSuccessesAfterLastFailureSQL = `WITH steps AS (
	SELECT r.workflow_id::text AS workflow_id, l.node_id, l.status, l.executed_at
	FROM workflow_runs r
	JOIN workflow_run_logs l ON l.run_id = r.id
	WHERE r.workflow_id IN ? AND r.updated_at >= ?
	  AND (r.workflow_id::text, l.node_id) IN ?
	  AND l.executed_at >= ? AND l.status IN ('executed', 'failed')
), last_failure AS (
	SELECT workflow_id, node_id, MAX(executed_at) AS failed_at
	FROM steps WHERE status = 'failed'
	GROUP BY workflow_id, node_id
)
SELECT s.workflow_id, s.node_id, COUNT(*) AS successes
FROM steps s
LEFT JOIN last_failure f ON f.workflow_id = s.workflow_id AND f.node_id = s.node_id
WHERE s.status = 'executed' AND (f.failed_at IS NULL OR s.executed_at > f.failed_at)
GROUP BY s.workflow_id, s.node_id`

const maxWorkflowAlerts = 200

const maxAlertSignalGroups = 1000

type RuntimeAlertRepository struct {
	db *gorm.DB
}

func NewRuntimeAlertRepository(db *gorm.DB) *RuntimeAlertRepository {
	return &RuntimeAlertRepository{db: db}
}

var (
	_ workflow.RuntimeAlertRepository = (*RuntimeAlertRepository)(nil)
	_ workflow.RuntimeAlertSignals    = (*RuntimeAlertRepository)(nil)
)

type runtimeAlertRow struct {
	ID          string
	WorkspaceID string
	WorkflowID  string
	NodeID      string
	AgentID     string
	Subject     string
	Kind        string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Sample      string
	Status      string
	ResolvedAt  *time.Time
	NotifiedAt  *time.Time
}

func (row runtimeAlertRow) domain() workflow.RuntimeAlert {
	return workflow.RuntimeAlert{
		ID: row.ID,
		Key: workflow.RuntimeAlertKey{
			WorkspaceID: row.WorkspaceID, WorkflowID: row.WorkflowID, NodeID: row.NodeID,
			AgentID: row.AgentID, Subject: row.Subject, Kind: workflow.RuntimeAlertKind(row.Kind),
		},
		Count:      row.Count,
		FirstSeen:  row.FirstSeen,
		LastSeen:   row.LastSeen,
		Sample:     row.Sample,
		Status:     workflow.RuntimeAlertStatus(row.Status),
		ResolvedAt: row.ResolvedAt,
		NotifiedAt: row.NotifiedAt,
	}.Described()
}

func alertsOf(rows []runtimeAlertRow) []workflow.RuntimeAlert {
	out := make([]workflow.RuntimeAlert, len(rows))
	for i, row := range rows {
		out[i] = row.domain()
	}
	return out
}

func alertRowArgs(id string, key workflow.RuntimeAlertKey, count int, first, last time.Time, sample string) []interface{} {
	return []interface{}{id, key.WorkspaceID, key.WorkflowID, key.NodeID, key.AgentID, key.Subject, string(key.Kind), count, first, last, sample}
}

func (r *RuntimeAlertRepository) RecordRuntimeAlert(s workflow.RuntimeAlertSignal) error {
	if !s.Valid() {
		return nil
	}
	return r.db.Exec(recordRuntimeAlertSQL, alertRowArgs(uuid.New().String(), s.Key, s.Count, s.FirstSeen, s.LastSeen, s.Sample)...).Error
}

func (r *RuntimeAlertRepository) OpenAlerts(alerts []workflow.RuntimeAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	rows := make([]string, len(alerts))
	args := make([]interface{}, 0, len(alerts)*11)
	for i, a := range alerts {
		rows[i] = openRuntimeAlertRow
		args = append(args, alertRowArgs(uuid.New().String(), a.Key, a.Count, a.FirstSeen, a.LastSeen, a.Sample)...)
	}
	return r.db.Exec(openRuntimeAlertsPrefix+strings.Join(rows, ", ")+openRuntimeAlertsSuffix, args...).Error
}

func (r *RuntimeAlertRepository) TouchAlerts(alerts []workflow.RuntimeAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	rows := make([]string, len(alerts))
	args := make([]interface{}, 0, len(alerts)*4)
	for i, a := range alerts {
		rows[i] = touchRuntimeAlertRow
		args = append(args, a.ID, a.Count, a.LastSeen, a.Sample)
	}
	return r.db.Exec(touchRuntimeAlertsPrefix+strings.Join(rows, ", ")+touchRuntimeAlertsSuffix, args...).Error
}

func (r *RuntimeAlertRepository) ResolveAlerts(ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Exec(resolveRuntimeAlertsSQL, at, ids).Error
}

func (r *RuntimeAlertRepository) MarkNotified(ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Exec(markRuntimeAlertsNotifiedSQL, at, ids).Error
}

func (r *RuntimeAlertRepository) Tracked(resolvedSince time.Time, limit int) ([]workflow.RuntimeAlert, error) {
	var rows []runtimeAlertRow
	if err := r.db.Raw(trackedRuntimeAlertsSQL, resolvedSince, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return alertsOf(rows), nil
}

func (r *RuntimeAlertRepository) ListForWorkflow(workspaceID, workflowID string, visibility conversation.VisibilityScope, resolvedSince time.Time) ([]workflow.RuntimeAlert, error) {
	visible, visibleArgs := visibleAlertConversations("", workspaceID, visibility)
	args := append([]interface{}{workspaceID, workflowID, resolvedSince}, visibleArgs...)
	var rows []runtimeAlertRow
	if err := r.db.Raw(workflowRuntimeAlertsFrom+visible+workflowRuntimeAlertsOrder, append(args, maxWorkflowAlerts)...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return alertsOf(rows), nil
}

func (r *RuntimeAlertRepository) ListOpen(workspaceID string, departments workflow.DepartmentRestriction, visibility conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.RuntimeAlert], error) {
	pagination = shared.NormalizePagination(pagination)
	if departments.Nothing() {
		return shared.NewPaginatedResult([]*workflow.RuntimeAlert{}, pagination, 0), nil
	}
	from := openRuntimeAlertsFrom
	args := []interface{}{workspaceID}
	if departments.Restricted {
		from += openRuntimeAlertsInDepartments
		args = append(args, departments.DepartmentIDs)
	}
	visible, visibleArgs := visibleAlertConversations("a.", workspaceID, visibility)
	from += visible
	args = append(args, visibleArgs...)
	var total int64
	if err := r.db.Raw("SELECT COUNT(*)"+from, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	var rows []runtimeAlertRow
	page := "SELECT a." + strings.ReplaceAll(runtimeAlertColumns, ", ", ", a.") + from + "\nORDER BY a.last_seen DESC LIMIT ? OFFSET ?"
	if err := r.db.Raw(page, append(args, pagination.PageSize, pagination.Offset())...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*workflow.RuntimeAlert, len(rows))
	for i, row := range rows {
		alert := row.domain()
		items[i] = &alert
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *RuntimeAlertRepository) DeleteResolvedBefore(cutoff time.Time, limit int) (int64, error) {
	result := r.db.Exec(deleteResolvedRuntimeAlertsSQL, cutoff, limit)
	return result.RowsAffected, result.Error
}

type alertGroupRow struct {
	WorkspaceID string
	WorkflowID  string
	NodeID      string
	AgentID     string
	ToolName    string
	ErrorCode   string
	Reason      string
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Sample      *string
}

func (row alertGroupRow) sample() string {
	if row.Sample == nil {
		return ""
	}
	return *row.Sample
}

func (r *RuntimeAlertRepository) RejectedDeliveries(since time.Time) ([]workflow.RejectedDeliveryGroup, error) {
	var rows []alertGroupRow
	if err := r.db.Raw(rejectedDeliveriesSQL, since, maxAlertSignalGroups).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflow.RejectedDeliveryGroup, len(rows))
	for i, row := range rows {
		out[i] = workflow.RejectedDeliveryGroup{
			WorkspaceID: row.WorkspaceID, WorkflowID: row.WorkflowID, Reason: row.Reason,
			Count: row.Count, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen, Sample: row.sample(),
		}
	}
	return out, nil
}

func (r *RuntimeAlertRepository) FailedToolCalls(since time.Time) ([]workflow.FailedToolCallGroup, error) {
	var rows []alertGroupRow
	if err := r.db.Raw(failedToolCallsSQL, since, maxAlertSignalGroups).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflow.FailedToolCallGroup, len(rows))
	for i, row := range rows {
		out[i] = workflow.FailedToolCallGroup{
			WorkspaceID: row.WorkspaceID, AgentID: row.AgentID, ToolName: row.ToolName, ErrorCode: row.ErrorCode,
			Count: row.Count, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen, Sample: row.sample(),
		}
	}
	return out, nil
}

func (r *RuntimeAlertRepository) FailingNodes(since time.Time, lastExecutions int) ([]workflow.RuntimeAlertSignal, error) {
	var rows []alertGroupRow
	if err := r.db.Raw(failingNodesSQL, since, since, since, lastExecutions, maxAlertSignalGroups).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]workflow.RuntimeAlertSignal, len(rows))
	for i, row := range rows {
		out[i] = workflow.RuntimeAlertSignal{
			Key:   workflow.RuntimeAlertKey{WorkspaceID: row.WorkspaceID, WorkflowID: row.WorkflowID, NodeID: row.NodeID, Kind: workflow.AlertNodeFailing},
			Count: row.Count, FirstSeen: row.FirstSeen, LastSeen: row.LastSeen, Sample: row.sample(),
		}
	}
	return out, nil
}

type nodeSuccessRow struct {
	WorkflowID string
	NodeID     string
	Successes  int
}

func (r *RuntimeAlertRepository) NodeSuccessesAfterLastFailure(probes []workflow.NodeAlertProbe, since time.Time) (map[workflow.NodeAlertProbe]int, error) {
	out := map[workflow.NodeAlertProbe]int{}
	if len(probes) == 0 {
		return out, nil
	}
	workflowIDs := make([]string, 0, len(probes))
	seen := map[string]bool{}
	pairs := make([][]interface{}, len(probes))
	for i, p := range probes {
		pairs[i] = []interface{}{p.WorkflowID, p.NodeID}
		if !seen[p.WorkflowID] {
			seen[p.WorkflowID] = true
			workflowIDs = append(workflowIDs, p.WorkflowID)
		}
	}
	var rows []nodeSuccessRow
	if err := r.db.Raw(nodeSuccessesAfterLastFailureSQL, workflowIDs, since, pairs, since).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[workflow.NodeAlertProbe{WorkflowID: row.WorkflowID, NodeID: row.NodeID}] = row.Successes
	}
	return out, nil
}
