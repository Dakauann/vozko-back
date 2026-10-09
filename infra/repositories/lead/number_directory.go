package lead

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/infra/database/schema"
)

type NumberDirectory struct{ repo *repository }

func NewNumberDirectory(db *gorm.DB) *NumberDirectory {
	return &NumberDirectory{repo: &repository{db: db}}
}

func (d *NumberDirectory) FindByNumbers(workspaceID string, numbers []string) ([]*lead.Lead, error) {
	return d.repo.FindByNumbers(workspaceID, numbers)
}

func (d *NumberDirectory) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	return d.repo.FindByIDs(workspaceID, ids)
}

func (d *NumberDirectory) LoadForDial(ctx context.Context, workspaceID, leadID string) (*lead.Lead, error) {
	return d.repo.loadWith(ctx, workspaceID, leadID, phonesOnly)
}

func (d *NumberDirectory) LoadManyForDial(ctx context.Context, workspaceID string, ids []string) ([]*lead.Lead, error) {
	leads, err := d.repo.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(d.repo.db.WithContext(ctx), workspaceID, leads, phonesOnly); err != nil {
		return nil, err
	}
	return leads, nil
}

func (d *NumberDirectory) FindIdentities(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	formats := numberFormats(numbers)
	if len(formats) == 0 {
		return []*lead.Lead{}, nil
	}
	var rows []schema.Lead
	if err := d.repo.db.WithContext(ctx).Where("workspace_id = ? AND number = ANY(?::text[])", workspaceID, formats).Limit(directoryLookupLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return toDomainAll(rows)
}

const otherHoldersSQL = "SELECT * FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND leads.id IN (" +
	"SELECT h.id FROM unnest(?::text[]) AS f(number) CROSS JOIN LATERAL (" +
	"(SELECT l.id FROM leads l WHERE l.workspace_id = ? AND l.number = f.number AND l.deleted_at IS NULL AND l.id <> ?::uuid LIMIT ?)" +
	" UNION ALL (SELECT p.lead_id FROM lead_phones p JOIN leads pl ON pl.id = p.lead_id AND pl.deleted_at IS NULL" +
	" WHERE p.workspace_id = ? AND p.number = f.number AND p.lead_id <> ?::uuid ORDER BY p.lead_id LIMIT ?)) AS h(id))"

func (d *NumberDirectory) OtherHolders(ctx context.Context, workspaceID, leadID string, numbers []string) ([]*lead.Lead, error) {
	workspaceID, leadID = strings.TrimSpace(workspaceID), strings.TrimSpace(leadID)
	if workspaceID == "" {
		return nil, lead.ErrLeadWorkspaceRequired
	}
	if _, err := uuid.Parse(leadID); err != nil {
		return nil, lead.ErrLeadRequired
	}
	formats := numberFormats(numbers)
	if len(formats) == 0 {
		return []*lead.Lead{}, nil
	}
	perForm := lead.SharedNumberHolderLimit + 1
	db := d.repo.db.WithContext(ctx)
	var rows []schema.Lead
	if err := db.Raw(otherHoldersSQL, workspaceID, formats, workspaceID, leadID, perForm, workspaceID, leadID, perForm).Scan(&rows).Error; err != nil {
		return nil, err
	}
	holders, err := toDomainAll(rows)
	if err != nil {
		return nil, err
	}
	if err := attachCollections(db, workspaceID, holders, phonesOnly); err != nil {
		return nil, err
	}
	return holders, nil
}
