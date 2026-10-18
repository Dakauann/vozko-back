package leadexit_repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"vozko/domain/buttonaction"
	"vozko/domain/leadexit"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/exitcolumns"
)

const (
	claimRunSQL = `INSERT INTO lead_exit_runs AS r (workspace_id, inbound_message_id, entry_id, entry_type, lead_id, template_message_id, exit_trigger, kind,
			outcome, reason, assigned_user_id, consent_event_id, confirmation, confirmation_reason, claimed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'running', '', '', '', '', '', NOW(), NOW())
		ON CONFLICT (workspace_id, inbound_message_id) DO UPDATE SET entry_id = EXCLUDED.entry_id, entry_type = EXCLUDED.entry_type,
			lead_id = EXCLUDED.lead_id, template_message_id = EXCLUDED.template_message_id, exit_trigger = EXCLUDED.exit_trigger, kind = EXCLUDED.kind,
			outcome = 'running', reason = '', assigned_user_id = '', consent_event_id = '', confirmation = '', confirmation_reason = '',
			claimed_at = NOW(), updated_at = NOW()
		WHERE r.outcome = 'failed' OR (r.outcome = 'running' AND r.claimed_at <= NOW() - (? * INTERVAL '1 second'))
		RETURNING inbound_message_id`
	selectRunSQL = `SELECT workspace_id, inbound_message_id, entry_id, entry_type, lead_id, template_message_id, exit_trigger, kind, outcome, reason,
			assigned_user_id, consent_event_id, confirmation, confirmation_reason, claimed_at, updated_at
		FROM lead_exit_runs WHERE workspace_id = ? AND inbound_message_id = ?`
	finishRunSQL = `UPDATE lead_exit_runs SET outcome = ?, reason = ?, assigned_user_id = ?, consent_event_id = ?, confirmation = ?, confirmation_reason = ?,
			updated_at = NOW()
		WHERE workspace_id = ? AND inbound_message_id = ?`
)

type RunStore struct {
	db *gorm.DB
}

var _ leadexit.RunStore = (*RunStore)(nil)

func NewRunStore(db *gorm.DB) *RunStore {
	return &RunStore{db: db}
}

type claimedRow struct {
	InboundMessageID string
}

func (s *RunStore) Claim(ctx context.Context, run leadexit.Run) (leadexit.RunClaim, error) {
	if !exitcolumns.ValidID(run.WorkspaceID) {
		return leadexit.RunClaim{}, leadexit.ErrWorkspaceRequired
	}
	var claimed []claimedRow
	if err := s.db.WithContext(ctx).Raw(claimRunSQL, run.WorkspaceID, run.InboundMessageID, run.EntryID, run.EntryType, run.LeadID,
		run.TemplateMessageID, string(run.Trigger), string(run.Kind), leadexit.RunTimeout.Seconds()).Scan(&claimed).Error; err != nil {
		return leadexit.RunClaim{}, err
	}
	if len(claimed) == 1 {
		return leadexit.RunClaim{Claimed: true}, nil
	}
	var rows []schema.LeadExitRun
	if err := s.db.WithContext(ctx).Raw(selectRunSQL, run.WorkspaceID, run.InboundMessageID).Scan(&rows).Error; err != nil {
		return leadexit.RunClaim{}, err
	}
	if len(rows) != 1 {
		return leadexit.RunClaim{}, fmt.Errorf("lead exit run %s was neither claimed nor found", run.InboundMessageID)
	}
	return leadexit.RunClaim{Previous: runOf(rows[0])}, nil
}

func (s *RunStore) Finish(ctx context.Context, run leadexit.Run) error {
	if !exitcolumns.ValidID(run.WorkspaceID) {
		return leadexit.ErrWorkspaceRequired
	}
	result := s.db.WithContext(ctx).Exec(finishRunSQL, string(run.Outcome), run.Reason, run.AssignedUserID, run.ConsentEventID,
		string(run.Confirmation), run.ConfirmationReason, run.WorkspaceID, run.InboundMessageID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("lead exit run %s was never claimed", run.InboundMessageID)
	}
	return nil
}

func runOf(row schema.LeadExitRun) leadexit.Run {
	return leadexit.Run{
		WorkspaceID: row.WorkspaceID, InboundMessageID: row.InboundMessageID, EntryID: row.EntryID, EntryType: row.EntryType, LeadID: row.LeadID,
		TemplateMessageID: row.TemplateMessageID, Trigger: leadexit.Trigger(row.ExitTrigger), Kind: buttonaction.Kind(row.Kind),
		Outcome: leadexit.Outcome(row.Outcome), Reason: row.Reason, AssignedUserID: row.AssignedUserID, ConsentEventID: row.ConsentEventID,
		Confirmation: leadexit.ConfirmationState(row.Confirmation), ConfirmationReason: row.ConfirmationReason, UpdatedAt: row.UpdatedAt,
	}
}
