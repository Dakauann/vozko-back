package lead

import (
	"context"
	"time"

	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const (
	lockCompanySQL = "SELECT accepts_leads FROM lead_companies WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL FOR SHARE"

	saveMembershipSQL = "UPDATE leads SET company_id = ?::uuid, updated_at = ?, version = version + 1" +
		" WHERE id = ? AND workspace_id = ? AND version = ? AND deleted_at IS NULL RETURNING version"
)

func (r *repository) SaveMembership(ctx context.Context, l *lead.Lead, expectedVersion int64, events []recordevent.Event) error {
	if l == nil {
		return lead.ErrLeadRequired
	}
	if err := shared.RequireVersion(expectedVersion); err != nil {
		return err
	}
	if l.CompanyID != "" && len(database.UUIDArray([]string{l.CompanyID})) == 0 {
		return leadcompany.ErrNotFound
	}
	now := time.Now().UTC()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if l.CompanyID != "" {
			if err := admitInto(tx, l.WorkspaceID, l.CompanyID); err != nil {
				return err
			}
		}
		var versions []int64
		if err := tx.Raw(saveMembershipSQL, schema.OptionalText(l.CompanyID), now, l.ID, l.WorkspaceID, expectedVersion).Scan(&versions).Error; err != nil {
			return err
		}
		if len(versions) == 0 {
			return whyMembershipNotSaved(tx, l)
		}
		l.Version, l.UpdatedAt = versions[0], now
		return insertEvents(tx, l.WorkspaceID, l.ID, events, now)
	})
	if err != nil {
		return err
	}
	r.agg.bump(l.WorkspaceID)
	return nil
}

func admitInto(tx *gorm.DB, workspaceID, companyID string) error {
	var accepts []bool
	if err := tx.Raw(lockCompanySQL, workspaceID, companyID).Scan(&accepts).Error; err != nil {
		return err
	}
	switch {
	case len(accepts) == 0:
		return leadcompany.ErrNotFound
	case !accepts[0]:
		return leadcompany.ErrClosed
	}
	return nil
}

func whyMembershipNotSaved(tx *gorm.DB, l *lead.Lead) error {
	if _, err := storedVersion(tx, l); err != nil {
		return err
	}
	return shared.ErrVersionConflict
}
