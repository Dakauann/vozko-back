package emailsending_repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	es "vozko/domain/emailsending"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const healthWindowSQL = `SELECT
  COUNT(*) FILTER (WHERE m.delivered_at IS NOT NULL) AS delivered,
  COUNT(*) FILTER (WHERE m.bounce_kind = ?) AS hard_bounces,
  COUNT(*) FILTER (WHERE m.complained_at IS NOT NULL) AS complaints
FROM email_messages m
JOIN email_sending_domains d ON d.id = m.domain_id
WHERE m.workspace_id = ? AND m.sent_at >= ? AND d.kind = ?`

type messageRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewMessageRepository(db *gorm.DB) es.MessageRepository {
	return &messageRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *messageRepository) take(q *gorm.DB) (*es.Message, error) {
	var row schema.EmailMessage
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, es.ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}
	m := toMessage(row)
	return &m, nil
}

func (r *messageRepository) Record(ctx context.Context, m *es.Message) error {
	now := r.now()
	m.ID = newID(m.ID)
	row := toMessageSchema(m)
	row.CreatedAt, row.UpdatedAt = now, now
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *messageRepository) Get(ctx context.Context, id string) (*es.Message, error) {
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *messageRepository) GetByProviderID(ctx context.Context, providerMessageID string) (*es.Message, error) {
	if providerMessageID == "" {
		return nil, es.ErrMessageNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("provider_message_id = ?", providerMessageID))
}

func (r *messageRepository) Update(ctx context.Context, m *es.Message) error {
	updates := messageUpdates(m)
	updates["updated_at"] = r.now()
	return database.AffectedOne(r.db.WithContext(ctx).Model(&schema.EmailMessage{}).Where("id = ?", m.ID).Updates(updates), es.ErrMessageNotFound)
}

func LinkHash(link string) string {
	if link == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(link))
	return hex.EncodeToString(sum[:])
}

type eventDetail struct {
	Recipients []string `json:"recipients,omitempty"`
	BounceKind string   `json:"bounce_kind,omitempty"`
	SubType    string   `json:"bounce_sub_type,omitempty"`
	Diagnostic string   `json:"diagnostic,omitempty"`
	Feedback   string   `json:"feedback_type,omitempty"`
}

func detailOf(ev es.DeliveryEvent) (datatypes.JSON, error) {
	d := eventDetail{}
	for _, a := range ev.Recipients {
		d.Recipients = append(d.Recipients, string(a))
	}
	if ev.Bounce != nil {
		d.BounceKind, d.SubType, d.Diagnostic = string(ev.Bounce.Kind), ev.Bounce.SubType, ev.Bounce.Diagnostic
	}
	if ev.Complaint != nil {
		d.Feedback = ev.Complaint.FeedbackType
	}
	raw, err := json.Marshal(d)
	return datatypes.JSON(raw), err
}

func (r *messageRepository) AppendEvent(ctx context.Context, messageID string, ev es.DeliveryEvent) (es.AppendedEvent, error) {
	var out es.AppendedEvent
	detail, err := detailOf(ev)
	if err != nil {
		return out, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row schema.EmailMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", messageID).Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return es.ErrMessageNotFound
			}
			return err
		}
		event := schema.EmailMessageEvent{
			ID:                newID(""),
			MessageID:         row.ID,
			WorkspaceID:       row.WorkspaceID,
			ProviderMessageID: ev.ProviderMessageID,
			Kind:              string(ev.Kind),
			OccurredAt:        ev.OccurredAt.UTC(),
			LinkHash:          LinkHash(ev.Link),
			Link:              ev.Link,
			UserAgent:         ev.UserAgent,
			IsBot:             ev.IsBot,
			Detail:            detail,
			CreatedAt:         r.now(),
		}
		inserted := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if inserted.Error != nil {
			return inserted.Error
		}
		msg := toMessage(row)
		if inserted.RowsAffected == 0 {
			out = es.AppendedEvent{New: false, Message: msg}
			return nil
		}
		effect := es.ApplyEvent(&msg, ev)
		if effect.Changed {
			updates := messageUpdates(&msg)
			updates["updated_at"] = r.now()
			if err := tx.Model(&schema.EmailMessage{}).Where("id = ?", msg.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := countRunOutcome(tx, msg, effect, r.now()); err != nil {
			return err
		}
		out = es.AppendedEvent{New: true, Message: msg, Effect: effect}
		return nil
	})
	return out, err
}

func (r *messageRepository) HealthWindow(ctx context.Context, workspaceID string, since time.Time) (es.HealthWindow, error) {
	var row struct {
		Delivered   int64
		HardBounces int64
		Complaints  int64
	}
	err := r.db.WithContext(ctx).Raw(healthWindowSQL, string(es.BounceHard), workspaceID, since.UTC(), string(es.DomainSharedProbation)).Scan(&row).Error
	if err != nil {
		return es.HealthWindow{}, err
	}
	return es.HealthWindow{Delivered: row.Delivered, HardBounces: row.HardBounces, Complaints: row.Complaints}, nil
}
