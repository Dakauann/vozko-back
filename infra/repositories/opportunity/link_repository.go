package opportunity_repository

import (
	"gorm.io/gorm"

	"vozko/domain/opportunity"
	"vozko/infra/database/schema"
)

type linkRepository struct {
	db *gorm.DB
}

func NewLinkRepository(db *gorm.DB) opportunity.LinkRepository {
	return &linkRepository{db: db}
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
