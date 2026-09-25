package aichat_repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
)

type messageRepository struct {
	db *gorm.DB
}

func NewMessageRepository(db *gorm.DB) aichat.MessageRepository {
	return &messageRepository{db: db}
}

func (r *messageRepository) Create(message *aichat.Message) error {
	rec := toMessageSchema(message)
	if err := r.db.Create(rec).Error; err != nil {
		return err
	}
	message.ID = rec.ID
	message.CreatedAt = rec.CreatedAt
	return nil
}

func (r *messageRepository) ListByThread(input aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	q := r.db.Model(&schema.AIChatMessage{}).Where("thread_id = ?", input.ThreadID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var recs []schema.AIChatMessage
	err := q.Order("created_at ASC").
		Limit(input.Limit).
		Offset(input.Offset).
		Find(&recs).Error
	if err != nil {
		return nil, 0, err
	}

	out := make([]*aichat.Message, len(recs))
	for i := range recs {
		out[i] = toMessageDomain(&recs[i])
	}
	return out, total, nil
}

func (r *messageRepository) DeleteByThread(threadID string) error {
	return r.db.Where("thread_id = ?", threadID).Delete(&schema.AIChatMessage{}).Error
}

func (r *messageRepository) ClaimProposal(threadID, proposalID string, outcome aichat.ProposalStatus) (*aichat.Message, error) {
	var recs []schema.AIChatMessage
	res := r.db.Model(&recs).
		Clauses(clause.Returning{}).
		Where("thread_id = ? AND proposal_id = ? AND proposal_status = ?", threadID, proposalID, aichat.ProposalPending).
		Update("proposal_status", string(outcome))
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 || len(recs) == 0 {
		return nil, aichat.ErrProposalNotPending
	}
	return toMessageDomain(&recs[0]), nil
}

func (r *messageRepository) ExpireProposals(threadID string) error {
	return r.db.Model(&schema.AIChatMessage{}).
		Where("thread_id = ? AND proposal_status = ?", threadID, aichat.ProposalPending).
		Update("proposal_status", string(aichat.ProposalExpired)).Error
}

func toMessageDomain(rec *schema.AIChatMessage) *aichat.Message {
	return &aichat.Message{
		ID:               rec.ID,
		ThreadID:         rec.ThreadID,
		Role:             aichat.Role(rec.Role),
		Content:          rec.Content,
		Model:            rec.Model,
		ToolCalls:        rec.ToolCalls,
		Attachments:      rec.Attachments,
		Reasoning:        rec.Reasoning,
		PromptTokens:     rec.PromptTokens,
		CompletionTokens: rec.CompletionTokens,
		CreatedAt:        rec.CreatedAt,
		ProposalID:       derefProposalID(rec.ProposalID),
		Proposal:         rec.Proposal,
		ProposalStatus:   aichat.ProposalStatus(rec.ProposalStatus),
	}
}

func derefProposalID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func proposalIDOf(m *aichat.Message) *string {
	if m.ProposalID == "" {
		return nil
	}
	id := m.ProposalID
	return &id
}

func toMessageSchema(m *aichat.Message) *schema.AIChatMessage {
	return &schema.AIChatMessage{
		ID:               m.ID,
		ThreadID:         m.ThreadID,
		Role:             string(m.Role),
		Content:          m.Content,
		Model:            m.Model,
		ToolCalls:        m.ToolCalls,
		Attachments:      m.Attachments,
		Reasoning:        m.Reasoning,
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		ProposalID:       proposalIDOf(m),
		Proposal:         m.Proposal,
		ProposalStatus:   string(m.ProposalStatus),
	}
}
