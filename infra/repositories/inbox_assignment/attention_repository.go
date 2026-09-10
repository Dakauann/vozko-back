package inbox_assignment_repository

import (
	"time"

	"gorm.io/gorm"

	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/infra/database/schema"
)

// attentionRepository answers "did anyone actually work this conversation after
// it was handed over".
//
// It is its own small repository rather than another method on
// conversation.MessageRepository: that interface is already twenty-odd methods
// wide and mocked in several packages, and widening it would make every one of
// those mocks fail to compile for a question none of them care about.
type attentionRepository struct {
	db *gorm.DB
}

func NewAttentionRepository(db *gorm.DB) ia.EntryAttentionReader {
	return &attentionRepository{db: db}
}

// AttendedSince reports whether the conversation was worked after `since`.
//
// Two signals, and each one is deliberately shaped:
//
//   - The OWNER marked a message read. read_by, not "read by anyone": a
//     supervisor skimming the inbox is not the assigned agent attending it.
//   - SOMEBODY answered. Any outbound message counts, including an AI reply.
//     The rescue exists so a customer is not left waiting; if the AI answered,
//     they are not. Escalating a conversation the AI is actively handling is a
//     different feature with its own hand-off machinery.
//
// direction = ” is the legacy-row fallback documented on the schema. Rows
// created after a rescue-eligible assignment always carry it, so the message
// type check is belt-and-braces rather than the main path.
func (r *attentionRepository) AttendedSince(entryID, entryType, assignedUserID string, since time.Time) (bool, error) {
	if entryID == "" || entryType == "" {
		return false, nil
	}

	inbound := conversation.InboundMessageTypeStrings()

	q := r.db.Model(&schema.ConversationMessage{}).
		Where("entry_id = ? AND entry_type = ?", entryID, entryType).
		Where(
			r.db.Where("read = ? AND read_by = ? AND read_at >= ?", true, assignedUserID, since).
				Or(
					r.db.Where("created_at >= ?", since).
						Where(
							r.db.Where("direction = ?", string(conversation.MessageDirectionOutbound)).
								Or("direction = ? AND message_type NOT IN ?", "", inbound),
						),
				),
		)

	var count int64
	if err := q.Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
