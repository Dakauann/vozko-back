package rag

import (
	"context"

	"vozko/domain/rag"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type agentKnowledgeBaseRepository struct {
	db *gorm.DB
}

func NewAgentKnowledgeBaseRepository(db *gorm.DB) rag.AgentKnowledgeBaseRepository {
	return &agentKnowledgeBaseRepository{db: db}
}

func (r *agentKnowledgeBaseRepository) Link(ctx context.Context, agentID string, knowledgeBaseID string) error {
	link := schema.AgentKnowledgeBase{
		AgentID:         agentID,
		KnowledgeBaseID: knowledgeBaseID,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error
}

func (r *agentKnowledgeBaseRepository) Unlink(ctx context.Context, agentID string, knowledgeBaseID string) error {
	return r.db.WithContext(ctx).
		Where("agent_id = ? AND knowledge_base_id = ?", agentID, knowledgeBaseID).
		Delete(&schema.AgentKnowledgeBase{}).Error
}

func (r *agentKnowledgeBaseRepository) FindByAgent(ctx context.Context, agentID string) ([]string, error) {
	var links []schema.AgentKnowledgeBase
	if err := r.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Find(&links).Error; err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.KnowledgeBaseID)
	}

	return ids, nil
}

func (r *agentKnowledgeBaseRepository) FindByKnowledgeBase(ctx context.Context, knowledgeBaseID string) ([]string, error) {
	var links []schema.AgentKnowledgeBase
	if err := r.db.WithContext(ctx).
		Where("knowledge_base_id = ?", knowledgeBaseID).
		Find(&links).Error; err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.AgentID)
	}

	return ids, nil
}

func (r *agentKnowledgeBaseRepository) IsLinked(ctx context.Context, agentID string, knowledgeBaseID string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&schema.AgentKnowledgeBase{}).
		Where("agent_id = ? AND knowledge_base_id = ?", agentID, knowledgeBaseID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *agentKnowledgeBaseRepository) ReplaceForAgent(ctx context.Context, agentID string, knowledgeBaseIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&schema.AgentKnowledgeBase{}).Error; err != nil {
			return err
		}

		if len(knowledgeBaseIDs) == 0 {
			return nil
		}

		links := make([]schema.AgentKnowledgeBase, 0, len(knowledgeBaseIDs))
		for _, kbID := range knowledgeBaseIDs {
			links = append(links, schema.AgentKnowledgeBase{
				AgentID:         agentID,
				KnowledgeBaseID: kbID,
			})
		}

		return tx.Create(&links).Error
	})
}
