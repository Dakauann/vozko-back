package conversation_repository

import (
	"errors"

	"gorm.io/gorm"

	"vozko/domain/conversation"
)

var errUnattributedWithoutMessageID = errors.New("unattributed service message without a provider message id")

type unattributedServiceMessageRepository struct {
	db *gorm.DB
}

func NewUnattributedServiceMessageRepository(db *gorm.DB) conversation.UnattributedServiceMessageRepository {
	return &unattributedServiceMessageRepository{db: db}
}

const recordUnattributedSQL = `INSERT INTO whatsapp_unattributed_service_messages
	(whatsapp_message_id, phone_number_id, status, category, pricing_type, first_seen_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (whatsapp_message_id) DO UPDATE SET status = EXCLUDED.status,
	phone_number_id = COALESCE(NULLIF(EXCLUDED.phone_number_id, ''), whatsapp_unattributed_service_messages.phone_number_id),
	pricing_type = COALESCE(NULLIF(EXCLUDED.pricing_type, ''), whatsapp_unattributed_service_messages.pricing_type),
	last_seen_at = EXCLUDED.last_seen_at`

func (r *unattributedServiceMessageRepository) Record(m conversation.UnattributedServiceMessage) error {
	if m.WhatsAppMessageID == "" {
		return errUnattributedWithoutMessageID
	}
	return r.db.Exec(recordUnattributedSQL,
		m.WhatsAppMessageID, m.PhoneNumberID, string(m.Status), m.Category, m.PricingType, m.SeenAt, m.SeenAt,
	).Error
}
