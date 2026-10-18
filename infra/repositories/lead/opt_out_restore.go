package lead

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/recordevent"
	"vozko/infra/database/schema"
)

const optOutRestoreColumns = "SELECT leads.id::text AS id, leads.workspace_id::text AS workspace_id, leads.version," +
	" leads.whatsapp_opt_in_at, leads.whatsapp_opt_in_source, leads.whatsapp_opt_in_purpose," +
	" leads.opted_out_at, leads.opted_out_source, leads.whatsapp_consent_record"

var (
	optOutRestoreReadSQL = optOutRestoreColumns + bulkTargetSQL + " ORDER BY leads.id"
	optOutRestoreLockSQL = optOutRestoreReadSQL + " FOR UPDATE"
)

const optOutRestoreUpdateSQL = "UPDATE leads SET whatsapp_opt_in_at = v.opt_in_at, whatsapp_opt_in_source = v.opt_in_source," +
	" whatsapp_opt_in_purpose = v.opt_in_purpose, opted_out_at = NULL, opted_out_source = NULL, whatsapp_consent_record = v.record," +
	" version = leads.version + 1, updated_at = ?" +
	" FROM unnest(?::uuid[], ?::bigint[], ?::timestamptz[], ?::text[], ?::text[], ?::jsonb[]) AS v(id, version, opt_in_at, opt_in_source, opt_in_purpose, record)" +
	" WHERE leads.workspace_id = ? AND leads.id = v.id AND leads.version = v.version AND leads.deleted_at IS NULL" +
	" RETURNING leads.id::text AS id, leads.version"

var errRestoreChosenAtMissing = errors.New("lead opt-out restore: the moment the opt-outs were chosen is required")

type plannedRestore struct {
	after *lead.Lead
	event recordevent.Event
}

type restorePlan struct {
	restores []plannedRestore
	tally    leadaction.Tally
}

func (r *repository) applyOptOutRestores(ctx context.Context, workspaceID string, ids []string, w leadaction.BatchWrite) (leadaction.Tally, error) {
	if w.Edit.ChosenAt.IsZero() {
		return leadaction.Tally{}, errRestoreChosenAtMissing
	}
	at := w.At.UTC()
	var tally leadaction.Tally
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []schema.Lead
		if err := tx.Raw(optOutRestoreLockSQL, workspaceID, pq.StringArray(ids)).Scan(&rows).Error; err != nil {
			return err
		}
		plan, err := r.planRestores(rows, w, at)
		if err != nil {
			return err
		}
		tally = plan.tally
		tally.Gone = len(ids) - len(rows)
		if len(plan.restores) == 0 {
			return nil
		}
		changed, err := writeRestores(tx, workspaceID, plan.restores, at)
		if err != nil {
			return err
		}
		tally.Changed = changed
		tally.Unchanged += len(plan.restores) - len(changed)
		return nil
	})
	if err != nil {
		return leadaction.Tally{}, fmt.Errorf("lead opt-out restore: %w", err)
	}
	if len(tally.Changed) > 0 {
		r.agg.bump(workspaceID)
	}
	return tally, nil
}

func (r *repository) planRestores(rows []schema.Lead, w leadaction.BatchWrite, at time.Time) (restorePlan, error) {
	var plan restorePlan
	for i := range rows {
		before, err := toDomain(&rows[i])
		if err != nil {
			return restorePlan{}, err
		}
		withdrawal, restorable, err := withdrawalToRevert(before, w.Edit.ChosenAt, &plan.tally)
		if err != nil {
			return restorePlan{}, err
		}
		if !restorable {
			continue
		}
		change := w.Edit.RevertChange(w.ActorID, withdrawal, at)
		change.ID = r.leadID()
		after := *before
		if _, err := after.ApplyConsentChange(change); err != nil {
			return restorePlan{}, err
		}
		plan.restores = append(plan.restores, plannedRestore{after: &after, event: lead.ConsentEvent(change, before, &after)})
	}
	return plan, nil
}

func withdrawalToRevert(l *lead.Lead, chosenAt time.Time, tally *leadaction.Tally) (string, bool, error) {
	withdrawal, err := l.WithdrawalToRevert(chosenAt)
	switch {
	case errors.Is(err, lead.ErrLeadNotOptedOut):
		tally.Unchanged++
		return "", false, nil
	case errors.Is(err, lead.ErrOptOutNotRestorable):
		tally.NotRestorable++
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return withdrawal, true, nil
}

func restoreUpdate(workspaceID string, restores []plannedRestore, at time.Time) (string, []interface{}, error) {
	ids := make(pq.StringArray, 0, len(restores))
	versions := make(pq.Int64Array, 0, len(restores))
	optInAt := make([]sql.NullString, 0, len(restores))
	optInSource := make([]sql.NullString, 0, len(restores))
	optInPurpose := make([]sql.NullString, 0, len(restores))
	records := make(pq.StringArray, 0, len(restores))
	for _, restore := range restores {
		l := restore.after
		grantedAt, source, purpose := consentColumns(l.WhatsAppOptIn)
		ids, versions = append(ids, l.ID), append(versions, l.Version)
		optInAt = append(optInAt, nullTime(grantedAt))
		optInSource = append(optInSource, nullText(string(source)))
		optInPurpose = append(optInPurpose, nullText(string(purpose)))
		record, err := consentRecordColumn(l.ConsentRecord)
		if err != nil {
			return "", nil, err
		}
		records = append(records, string(record))
	}
	return optOutRestoreUpdateSQL, []interface{}{
		at, ids, versions, pq.Array(optInAt), pq.Array(optInSource), pq.Array(optInPurpose), records, workspaceID,
	}, nil
}

func writeRestores(tx *gorm.DB, workspaceID string, restores []plannedRestore, at time.Time) ([]leadaction.Changed, error) {
	sql, args, err := restoreUpdate(workspaceID, restores, at)
	if err != nil {
		return nil, err
	}
	var changed []bulkChangedRow
	if err := tx.Raw(sql, args...).Scan(&changed).Error; err != nil {
		return nil, err
	}
	written := make(map[string]int64, len(changed))
	for _, row := range changed {
		written[row.ID] = row.Version
	}
	out := make([]leadaction.Changed, 0, len(changed))
	events := make([]schema.LeadEvent, 0, len(changed))
	for _, restore := range restores {
		version, ok := written[restore.after.ID]
		if !ok {
			continue
		}
		rows, err := eventRows(workspaceID, restore.after.ID, []recordevent.Event{restore.event}, at)
		if err != nil {
			return nil, err
		}
		events = append(events, rows...)
		out = append(out, leadaction.Changed{LeadID: restore.after.ID, Version: version})
	}
	return out, insertEventRows(tx, events)
}

func (r *repository) tallyOptOutRestores(ctx context.Context, workspaceID string, ids []string, e leadaction.Edit) (leadaction.Tally, error) {
	if e.ChosenAt.IsZero() {
		return leadaction.Tally{}, errRestoreChosenAtMissing
	}
	var rows []schema.Lead
	if err := r.db.WithContext(ctx).Raw(optOutRestoreReadSQL, workspaceID, pq.StringArray(ids)).Scan(&rows).Error; err != nil {
		return leadaction.Tally{}, fmt.Errorf("lead opt-out restore tally: %w", err)
	}
	tally := leadaction.Tally{Gone: len(ids) - len(rows)}
	for i := range rows {
		l, err := toDomain(&rows[i])
		if err != nil {
			return leadaction.Tally{}, err
		}
		if _, _, err := withdrawalToRevert(l, e.ChosenAt, &tally); err != nil {
			return leadaction.Tally{}, err
		}
	}
	return tally, nil
}

func nullTime(at *time.Time) sql.NullString {
	if at == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: at.UTC().Format(time.RFC3339Nano), Valid: true}
}
