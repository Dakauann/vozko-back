package aichat_repository

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/aichat"
	"vozko/infra/database/schema"
)

type queueRepository struct {
	db *gorm.DB
}

func NewQueueRepository(db *gorm.DB) aichat.QueueRepository {
	return &queueRepository{db: db}
}

func (r *queueRepository) Enqueue(message *aichat.QueuedMessage) error {
	rec := &schema.AIChatQueuedMessage{ThreadID: message.ThreadID, ClientID: message.ClientID, Content: message.Content, Attachments: message.Attachments}
	res := r.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "thread_id"}, {Name: "client_id"}}, DoNothing: true}).Create(rec)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		rec = &schema.AIChatQueuedMessage{}
		if err := r.db.Where("thread_id = ? AND client_id = ?", message.ThreadID, message.ClientID).Take(rec).Error; err != nil {
			return err
		}
	}
	*message = *toQueuedDomain(rec)
	return nil
}

func (r *queueRepository) Pending(threadID string) ([]*aichat.QueuedMessage, error) {
	var recs []schema.AIChatQueuedMessage
	if err := r.db.Where("thread_id = ? AND delivered_at IS NULL", threadID).Order("created_at ASC, id ASC").Find(&recs).Error; err != nil {
		return nil, err
	}
	return toQueuedDomains(recs), nil
}

func (r *queueRepository) Remove(threadID, clientID string) error {
	res := r.db.Where("thread_id = ? AND client_id = ? AND delivered_at IS NULL", threadID, clientID).Delete(&schema.AIChatQueuedMessage{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return aichat.ErrQueuedMessageGone
	}
	return nil
}

func (r *queueRepository) Deliver(threadID string) ([]*aichat.QueuedMessage, error) {
	var recs []schema.AIChatQueuedMessage
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("thread_id = ? AND delivered_at IS NULL", threadID).
			Order("created_at ASC, id ASC").
			Find(&recs).Error; err != nil {
			return err
		}
		now := time.Now()
		for i := range recs {
			msg := toMessageSchema(toQueuedDomain(&recs[i]).Message())
			msg.CreatedAt = now.Add(time.Duration(i) * time.Microsecond)
			if err := tx.Create(msg).Error; err != nil {
				return err
			}
			deliveredAt := msg.CreatedAt
			recs[i].MessageID, recs[i].DeliveredAt = &msg.ID, &deliveredAt
			if err := tx.Model(&schema.AIChatQueuedMessage{}).Where("id = ?", recs[i].ID).
				Updates(map[string]interface{}{"message_id": msg.ID, "delivered_at": deliveredAt}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return toQueuedDomains(recs), nil
}

func toQueuedDomains(recs []schema.AIChatQueuedMessage) []*aichat.QueuedMessage {
	out := make([]*aichat.QueuedMessage, len(recs))
	for i := range recs {
		out[i] = toQueuedDomain(&recs[i])
	}
	return out
}

func toQueuedDomain(rec *schema.AIChatQueuedMessage) *aichat.QueuedMessage {
	out := &aichat.QueuedMessage{
		ID:          rec.ID,
		ThreadID:    rec.ThreadID,
		ClientID:    rec.ClientID,
		Content:     rec.Content,
		Attachments: rec.Attachments,
		CreatedAt:   rec.CreatedAt,
		DeliveredAt: rec.DeliveredAt,
	}
	if rec.MessageID != nil {
		out.MessageID = *rec.MessageID
	}
	return out
}
