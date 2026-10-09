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

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	activeImportStatusesSQL = "('analyzing', 'importing')"

	importGetSQL = "SELECT * FROM lead_imports WHERE workspace_id = ? AND id = ?"

	importSaveSQL = "UPDATE lead_imports SET status = ?, stage = ?, requested_by_admin = ?, settings = ?::jsonb, grants = ?::jsonb, fingerprint = ?," +
		" dry_run = ?::jsonb, result = ?::jsonb, seed = ?::jsonb, processed = ?, failure_code = ?, attempts = ?, claim_token = ?," +
		" heartbeat_at = ?, started_at = ?, finished_at = ?, updated_at = ?" +
		" WHERE id = ? AND status = ANY(?)"

	importClaimGuardSQL = " AND claim_token = ?"

	importClaimSQL = "UPDATE lead_imports SET claim_token = ?, heartbeat_at = ?, attempts = attempts + 1, updated_at = ?" +
		" WHERE id = ? AND status IN " + activeImportStatusesSQL + " AND attempts < ?" +
		" AND " + database.LeaseFreeSQL + " RETURNING *"

	importClaimableSQL = "SELECT id::text AS id, workspace_id::text AS workspace_id FROM lead_imports WHERE status IN " + activeImportStatusesSQL + " AND attempts < ?" +
		" AND " + database.LeaseFreeSQL + " ORDER BY created_at LIMIT ?"

	importFailStalledSQL = "WITH stalled AS (SELECT id, status FROM lead_imports WHERE status IN " + activeImportStatusesSQL +
		" AND attempts >= ? AND (heartbeat_at IS NULL OR heartbeat_at < ?) FOR UPDATE)" +
		" UPDATE lead_imports SET status = 'failed', failure_code = 'stalled', claim_token = NULL, finished_at = ?, updated_at = ?" +
		" FROM stalled WHERE lead_imports.id = stalled.id" +
		" RETURNING lead_imports.id::text AS id, lead_imports.workspace_id::text AS workspace_id, stalled.status AS status"

	importExpiredSQL = "SELECT * FROM lead_imports WHERE expires_at < ? AND status NOT IN " + activeImportStatusesSQL + " ORDER BY expires_at LIMIT ?"

	importDeleteSQL = "DELETE FROM lead_imports WHERE id = ?"

	importDeleteIssuesSQL = "DELETE FROM lead_import_issues WHERE import_id = ?"

	importDeleteAllLinksSQL = "DELETE FROM lead_import_links WHERE import_id = ?"

	importIssuesSQL = "SELECT seq, line, reason, field, rejected FROM lead_import_issues WHERE import_id = ? AND seq > ? ORDER BY seq LIMIT ?"

	importUnusedSQL = "SELECT * FROM lead_imports WHERE workspace_id = ? AND requested_by = ?" +
		" AND (status IN ('uploaded', 'analyzed') OR (status = 'failed' AND started_at IS NULL)) ORDER BY created_at LIMIT ?"

	importMineSQL = "SELECT * FROM lead_imports WHERE workspace_id = ? AND requested_by = ? AND expires_at > ? ORDER BY created_at DESC LIMIT ?"

	maxIssuePage = 5000

	maxUnusedRead = 50
)

func (s *Imports) Create(ctx context.Context, j *leadimport.Job) error {
	row, err := importRow(j)
	if err != nil {
		return err
	}
	if err := s.r.db.WithContext(ctx).Create(row).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return leadimport.ErrRunning
		}
		return err
	}
	return nil
}

func (s *Imports) Get(ctx context.Context, workspaceID, id string) (*leadimport.Job, error) {
	workspaceID, id = strings.TrimSpace(workspaceID), strings.TrimSpace(id)
	if workspaceID == "" || len(database.UUIDArray([]string{id})) == 0 {
		return nil, leadimport.ErrNotFound
	}
	var rows []schema.LeadImport
	if err := s.r.db.WithContext(ctx).Raw(importGetSQL, workspaceID, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, leadimport.ErrNotFound
	}
	return importOf(rows[0])
}

func (s *Imports) Save(ctx context.Context, j *leadimport.Job, guard leadimport.Guard) error {
	row, err := importRow(j)
	if err != nil {
		return err
	}
	from := make(pq.StringArray, len(guard.From))
	for i, status := range guard.From {
		from[i] = string(status)
	}
	sql := importSaveSQL
	args := []interface{}{
		row.Status, row.Stage, row.RequestedByAdmin, jsonArg(row.Settings), jsonArg(row.Grants), row.Fingerprint,
		jsonArg(row.DryRun), jsonArg(row.Result), jsonArg(row.Seed), row.Processed, row.FailureCode, row.Attempts, row.ClaimToken,
		row.HeartbeatAt, row.StartedAt, row.FinishedAt, row.UpdatedAt, row.ID, from,
	}
	if guard.Claim != "" {
		sql += importClaimGuardSQL
		args = append(args, guard.Claim)
	}
	sql += " RETURNING id"
	var ids []string
	if err := s.r.db.WithContext(ctx).Raw(sql, args...).Scan(&ids).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return leadimport.ErrRunning
		}
		return err
	}
	if len(ids) == 0 {
		if guard.Claim != "" {
			return leadimport.ErrClaimLost
		}
		return leadimport.ErrNotReady
	}
	return nil
}

func (s *Imports) Claim(ctx context.Context, id, token string, now time.Time) (*leadimport.Job, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, leadimport.ErrNotFound
	}
	now = now.UTC()
	var rows []schema.LeadImport
	if err := s.r.db.WithContext(ctx).Raw(importClaimSQL, token, now, now, id, leadimport.MaxAttempts, now.Add(-leadimport.StaleAfter)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, leadimport.ErrClaimLost
	}
	return importOf(rows[0])
}

type importRefRow struct {
	ID          string
	WorkspaceID string
	Status      string
}

func (s *Imports) Claimable(ctx context.Context, now time.Time, limit int) ([]leadimport.Ref, error) {
	var rows []importRefRow
	if err := s.r.db.WithContext(ctx).Raw(importClaimableSQL, leadimport.MaxAttempts, now.UTC().Add(-leadimport.StaleAfter), limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	refs := make([]leadimport.Ref, len(rows))
	for i, row := range rows {
		refs[i] = leadimport.Ref{ID: row.ID, WorkspaceID: row.WorkspaceID}
	}
	return refs, nil
}

func (s *Imports) FailStalled(ctx context.Context, now time.Time) ([]leadimport.Stalled, error) {
	now = now.UTC()
	var rows []importRefRow
	if err := s.r.db.WithContext(ctx).Raw(importFailStalledSQL, leadimport.MaxAttempts, now.Add(-leadimport.StaleAfter), now, now).Scan(&rows).Error; err != nil {
		return nil, err
	}
	stalled := make([]leadimport.Stalled, len(rows))
	for i, row := range rows {
		stalled[i] = leadimport.Stalled{Ref: leadimport.Ref{ID: row.ID, WorkspaceID: row.WorkspaceID}, Status: leadimport.Status(row.Status)}
	}
	return stalled, nil
}

func (s *Imports) Expired(ctx context.Context, now time.Time, limit int) ([]leadimport.Job, error) {
	var rows []schema.LeadImport
	if err := s.r.db.WithContext(ctx).Raw(importExpiredSQL, now.UTC(), limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	jobs := make([]leadimport.Job, 0, len(rows))
	for _, row := range rows {
		j, err := importOf(row)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, nil
}

func (s *Imports) Delete(ctx context.Context, id string) error {
	return s.r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, sql := range []string{importDeleteIssuesSQL, importDeleteAllLinksSQL, importDeleteSQL} {
			if err := tx.Exec(sql, id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Imports) Unused(ctx context.Context, workspaceID, requestedBy string) ([]leadimport.Job, error) {
	workspaceID, requestedBy = strings.TrimSpace(workspaceID), strings.TrimSpace(requestedBy)
	if workspaceID == "" || requestedBy == "" {
		return nil, leadimport.ErrNotFound
	}
	var rows []schema.LeadImport
	if err := s.r.db.WithContext(ctx).Raw(importUnusedSQL, workspaceID, requestedBy, maxUnusedRead).Scan(&rows).Error; err != nil {
		return nil, err
	}
	jobs := make([]leadimport.Job, 0, len(rows))
	for _, row := range rows {
		j, err := importOf(row)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, nil
}

type issueRow struct {
	Seq      int64
	Line     int
	Reason   string
	Field    schema.OptionalText
	Rejected bool
}

func (s *Imports) Issues(ctx context.Context, importID string, after int64, limit int) ([]leadimport.IssueRow, error) {
	if len(database.UUIDArray([]string{importID})) == 0 {
		return nil, leadimport.ErrNotFound
	}
	limit = max(1, min(limit, maxIssuePage))
	var rows []issueRow
	if err := s.r.db.WithContext(ctx).Raw(importIssuesSQL, importID, after, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]leadimport.IssueRow, len(rows))
	for i, row := range rows {
		out[i] = leadimport.IssueRow{Seq: row.Seq, ImportIssue: lead.ImportIssue{
			Line: row.Line, Reason: lead.RejectReason(row.Reason), Field: string(row.Field), Rejected: row.Rejected,
		}}
	}
	return out, nil
}

func jsonColumn(value any, present bool) (datatypes.JSON, error) {
	if !present {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("lead import column: %w", err)
	}
	return datatypes.JSON(raw), nil
}

func importRow(j *leadimport.Job) (*schema.LeadImport, error) {
	if j == nil {
		return nil, leadimport.ErrNotFound
	}
	preview, err := jsonColumn(j.Preview, true)
	if err != nil {
		return nil, err
	}
	settings, err := jsonColumn(j.Settings, j.Settings != nil)
	if err != nil {
		return nil, err
	}
	grants, err := jsonColumn(j.Grants, true)
	if err != nil {
		return nil, err
	}
	dryRun, err := jsonColumn(j.DryRun, j.DryRun != nil)
	if err != nil {
		return nil, err
	}
	result, err := jsonColumn(j.Result, j.Result != nil)
	if err != nil {
		return nil, err
	}
	seed, err := jsonColumn(j.Seed, j.Seed != nil)
	if err != nil {
		return nil, err
	}
	return &schema.LeadImport{
		ID: j.ID, WorkspaceID: j.WorkspaceID, RequestedBy: j.RequestedBy, RequestedByAdmin: j.RequestedByAdmin,
		Status: string(j.Status), Stage: schema.OptionalText(j.Stage), MediaID: schema.OptionalText(j.File.MediaID),
		FileName: j.File.Name, FileOwned: j.File.Owned, SizeBytes: j.File.SizeBytes, TotalRows: j.TotalRows,
		Preview: preview, Settings: settings, Grants: grants, Fingerprint: schema.OptionalText(j.Fingerprint),
		DryRun: dryRun, Result: result, Seed: seed, Processed: j.Processed, FailureCode: schema.OptionalText(j.FailureCode),
		Attempts: j.Attempts, ClaimToken: schema.OptionalText(j.Claim), HeartbeatAt: j.HeartbeatAt, StartedAt: j.StartedAt,
		FinishedAt: j.FinishedAt, ExpiresAt: j.ExpiresAt, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}, nil
}

func decodeColumn(id, name string, raw datatypes.JSON, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("lead import %s %s: %w", id, name, err)
	}
	return nil
}

func importOf(row schema.LeadImport) (*leadimport.Job, error) {
	j := &leadimport.Job{
		ID: row.ID, WorkspaceID: row.WorkspaceID, RequestedBy: row.RequestedBy, RequestedByAdmin: row.RequestedByAdmin,
		Status: leadimport.Status(row.Status), Stage: leadimport.Stage(row.Stage),
		File:      leadimport.File{MediaID: string(row.MediaID), Name: row.FileName, SizeBytes: row.SizeBytes, Owned: row.FileOwned},
		TotalRows: row.TotalRows, Fingerprint: string(row.Fingerprint), Processed: row.Processed,
		FailureCode: leadimport.FailureCode(row.FailureCode), Attempts: row.Attempts, Claim: string(row.ClaimToken),
		HeartbeatAt: row.HeartbeatAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := decodeColumn(row.ID, "preview", row.Preview, &j.Preview); err != nil {
		return nil, err
	}
	if err := decodeColumn(row.ID, "grants", row.Grants, &j.Grants); err != nil {
		return nil, err
	}
	optional := []struct {
		name string
		raw  datatypes.JSON
		set  func() any
	}{
		{"settings", row.Settings, func() any { j.Settings = &leadimport.Settings{}; return j.Settings }},
		{"dry run", row.DryRun, func() any { j.DryRun = &leadimport.Counts{}; return j.DryRun }},
		{"result", row.Result, func() any { j.Result = &leadimport.Counts{}; return j.Result }},
		{"seed", row.Seed, func() any { j.Seed = &leadimport.SeedOutcome{}; return j.Seed }},
	}
	for _, o := range optional {
		if len(o.raw) == 0 || string(o.raw) == "null" {
			continue
		}
		if err := decodeColumn(row.ID, o.name, o.raw, o.set()); err != nil {
			return nil, err
		}
	}
	return j, nil
}

func (s *Imports) Mine(ctx context.Context, workspaceID, requestedBy string, now time.Time, limit int) ([]leadimport.Job, error) {
	workspaceID, requestedBy = strings.TrimSpace(workspaceID), strings.TrimSpace(requestedBy)
	if workspaceID == "" || requestedBy == "" {
		return nil, leadimport.ErrNotFound
	}
	var rows []schema.LeadImport
	if err := s.r.db.WithContext(ctx).Raw(importMineSQL, workspaceID, requestedBy, now.UTC(), max(1, min(limit, leadimport.MaxListed))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	jobs := make([]leadimport.Job, 0, len(rows))
	for _, row := range rows {
		j, err := importOf(row)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, nil
}

var importPlacementSQL, importPlacementArgs = func() (string, []interface{}) {
	counts, args := placementCountsSQL(crmfilter.PlacementOnMap, crmfilter.PlacementApproximate, crmfilter.PlacementNotFound,
		crmfilter.PlacementQuotaExceeded, crmfilter.PlacementRefused, crmfilter.PlacementPending)
	return "SELECT " + strings.TrimPrefix(counts, ", ") + " FROM lead_addresses la" +
		" JOIN leads ON leads.id = la.lead_id AND leads.workspace_id = la.workspace_id AND leads.deleted_at IS NULL" +
		" WHERE la.workspace_id = ? AND la.import_id = ? AND la.is_primary", args
}()

type placementRow struct {
	OnMap, Approximate, NotFound, QuotaExceeded, Refused, Pending int
}

func (s *Imports) Placement(ctx context.Context, workspaceID, importID string) (leadimport.Placement, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || len(database.UUIDArray([]string{importID})) == 0 {
		return leadimport.Placement{}, leadimport.ErrNotFound
	}
	var row placementRow
	args := append(append([]interface{}{}, importPlacementArgs...), workspaceID, importID)
	if err := s.r.db.WithContext(ctx).Raw(importPlacementSQL, args...).Scan(&row).Error; err != nil {
		return leadimport.Placement{}, err
	}
	return leadimport.Placement{OnMap: row.OnMap, Approximate: row.Approximate, Pending: row.Pending,
		NotFound: row.NotFound, QuotaExceeded: row.QuotaExceeded, Refused: row.Refused}, nil
}
