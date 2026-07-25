package opportunity_repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
)

type linkRepository struct {
	db *gorm.DB
}

// NewLinkRepository returns the domain opportunity.LinkRepository backed by GORM.
// Links carry only (opportunity_id, entry_id, entry_type); workspace scoping on
// reads is enforced by joining to the opportunities table.
func NewLinkRepository(db *gorm.DB) opportunity.LinkRepository {
	return &linkRepository{db: db}
}

func (r *linkRepository) Link(link opportunity.ConversationLink) error {
	row := &schema.OpportunityConversation{
		OpportunityID: link.OpportunityID,
		EntryID:       link.EntryID,
		EntryType:     link.EntryType,
	}
	// Idempotent: a duplicate (opportunity, entry) is a no-op rather than an error.
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "opportunity_id"}, {Name: "entry_id"}, {Name: "entry_type"}},
		DoNothing: true,
	}).Create(row).Error
}

func (r *linkRepository) Unlink(opportunityID, entryID, entryType string) error {
	return r.db.
		Where("opportunity_id = ? AND entry_id = ? AND entry_type = ?", opportunityID, entryID, entryType).
		Delete(&schema.OpportunityConversation{}).Error
}

func (r *linkRepository) ListByOpportunity(workspaceID, opportunityID string) ([]opportunity.ConversationLink, error) {
	var rows []schema.OpportunityConversation
	if err := r.db.
		Table("opportunity_conversations AS oc").
		Joins("JOIN opportunities o ON o.id = oc.opportunity_id AND o.deleted_at IS NULL").
		Where("o.workspace_id = ? AND oc.opportunity_id = ?", workspaceID, opportunityID).
		Select("oc.*").
		Order("oc.created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapLinks(rows), nil
}

func (r *linkRepository) ListByEntry(workspaceID, entryID, entryType string) ([]opportunity.ConversationLink, error) {
	var rows []schema.OpportunityConversation
	if err := r.db.
		Table("opportunity_conversations AS oc").
		Joins("JOIN opportunities o ON o.id = oc.opportunity_id AND o.deleted_at IS NULL").
		Where("o.workspace_id = ? AND oc.entry_id = ? AND oc.entry_type = ?", workspaceID, entryID, entryType).
		Select("oc.*").
		Order("oc.created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapLinks(rows), nil
}

func mapLinks(rows []schema.OpportunityConversation) []opportunity.ConversationLink {
	out := make([]opportunity.ConversationLink, 0, len(rows))
	for i := range rows {
		out = append(out, opportunity.ConversationLink{
			OpportunityID: rows[i].OpportunityID,
			EntryID:       rows[i].EntryID,
			EntryType:     rows[i].EntryType,
		})
	}
	return out
}
