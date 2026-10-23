package emailapi_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailaddress"
	"vozko/domain/emailapi"
	"vozko/infra/database/schema"
)

type Sends struct {
	db *gorm.DB
}

var _ emailapi.SendStore = (*Sends)(nil)

func NewSends(db *gorm.DB) *Sends {
	return &Sends{db: db}
}

func (s *Sends) Claim(ctx context.Context, send emailapi.Send, messages []emailapi.Message, tags []emailapi.Tag) (emailapi.ClaimResult, error) {
	row, err := sendRow(send)
	if err != nil {
		return emailapi.ClaimResult{}, err
	}
	var result emailapi.ClaimResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "workspace_id"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing schema.EmailAPISend
			if err := tx.Where("workspace_id = ? AND idempotency_key = ?", send.WorkspaceID, send.IdempotencyKey).Take(&existing).Error; err != nil {
				return err
			}
			found, err := sendOf(existing)
			if err != nil {
				return err
			}
			result = emailapi.ClaimResult{Send: found}
			return nil
		}
		rows := make([]schema.EmailAPIMessage, len(messages))
		var tagRows []schema.EmailAPIMessageTag
		for i, m := range messages {
			rows[i] = messageRow(m)
			for _, t := range tags {
				tagRows = append(tagRows, schema.EmailAPIMessageTag{MessageRowID: m.ID, Name: t.Name, WorkspaceID: m.WorkspaceID, Value: t.Value, CreatedAt: m.CreatedAt})
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		if len(tagRows) > 0 {
			if err := tx.Create(&tagRows).Error; err != nil {
				return err
			}
		}
		result = emailapi.ClaimResult{Send: send, Created: true, Messages: messages}
		return nil
	})
	return result, err
}

func (s *Sends) TakeLease(ctx context.Context, workspaceID, sendID string, now, until time.Time) (bool, error) {
	res := s.db.WithContext(ctx).Model(&schema.EmailAPISend{}).
		Where("id = ? AND workspace_id = ? AND (lease_until IS NULL OR lease_until < ?)", sendID, workspaceID, now).
		Updates(map[string]any{"lease_until": until, "updated_at": now})
	return res.RowsAffected == 1, res.Error
}

func (s *Sends) ReleaseLease(ctx context.Context, sendID string) error {
	return s.db.WithContext(ctx).Model(&schema.EmailAPISend{}).Where("id = ?", sendID).Update("lease_until", nil).Error
}

func (s *Sends) Get(ctx context.Context, sendID string) (emailapi.Send, error) {
	var row schema.EmailAPISend
	if err := s.db.WithContext(ctx).Where("id = ?", sendID).Take(&row).Error; err != nil {
		return emailapi.Send{}, err
	}
	return sendOf(row)
}

func (s *Sends) Messages(ctx context.Context, sendID string) ([]emailapi.Message, error) {
	var rows []schema.EmailAPIMessage
	if err := s.db.WithContext(ctx).Where("send_id = ?", sendID).Order("position").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]emailapi.Message, len(rows))
	for i, r := range rows {
		out[i] = messageOf(r)
	}
	return out, nil
}

func (s *Sends) OpenRound(ctx context.Context, sendID string, round int, units, unitMicros, unitCostMicros int64, positions []int) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&schema.EmailAPISend{}).Where("id = ? AND round = ? AND round_settled", sendID, round-1).
			Updates(map[string]any{"round": round, "round_units": units, "round_unit_micros": unitMicros, "round_unit_cost_micros": unitCostMicros, "round_settled": false})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("%w: round %d of send %s is not the next one", emailapi.ErrSendInProgress, round, sendID)
		}
		return tx.Model(&schema.EmailAPIMessage{}).Where("send_id = ? AND position IN ?", sendID, positions).
			Updates(map[string]any{"round": round, "status": string(emailapi.StatusPending), "error_code": ""}).Error
	})
}

func (s *Sends) SettleRound(ctx context.Context, sendID string, round int) error {
	return s.db.WithContext(ctx).Model(&schema.EmailAPISend{}).Where("id = ? AND round = ?", sendID, round).Update("round_settled", true).Error
}

func (s *Sends) MarkSent(ctx context.Context, sendID string, position int, messageID string, status emailapi.MessageStatus) error {
	return s.db.WithContext(ctx).Model(&schema.EmailAPIMessage{}).Where("send_id = ? AND position = ?", sendID, position).
		Updates(map[string]any{"message_id": messageID, "status": string(status), "error_code": "", "updated_at": time.Now().UTC()}).Error
}

func (s *Sends) MarkRefused(ctx context.Context, sendID string, positions []int, code string) error {
	if len(positions) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&schema.EmailAPIMessage{}).Where("send_id = ? AND position IN ? AND message_id IS NULL", sendID, positions).
		Updates(map[string]any{"status": string(emailapi.StatusRefused), "error_code": code, "updated_at": time.Now().UTC()}).Error
}

const markStatusSQL = `UPDATE email_api_messages SET status = ?,
	last_event = CASE WHEN last_event_at IS NULL OR last_event_at <= ? THEN ? ELSE last_event END,
	last_event_at = CASE WHEN last_event_at IS NULL OR last_event_at <= ? THEN ? ELSE last_event_at END,
	updated_at = ?
	WHERE message_id = ?`

func (s *Sends) MarkStatus(ctx context.Context, messageID string, status emailapi.MessageStatus, event string, at time.Time) (bool, error) {
	res := s.db.WithContext(ctx).Exec(markStatusSQL, string(status), at, event, at, at, time.Now().UTC(), messageID)
	return res.RowsAffected > 0, res.Error
}

func sendRow(s emailapi.Send) (schema.EmailAPISend, error) {
	tags, err := json.Marshal(nonNilTags(s.Tags))
	if err != nil {
		return schema.EmailAPISend{}, err
	}
	now := s.CreatedAt
	return schema.EmailAPISend{
		ID: s.ID, WorkspaceID: s.WorkspaceID, IdempotencyKey: s.IdempotencyKey, Fingerprint: s.Fingerprint, Origin: string(s.Origin),
		APIKeyID: schema.OptionalText(s.APIKeyID), SenderID: s.SenderID, FromAddress: string(s.From), Subject: s.Subject,
		Tags: datatypes.JSON(tags), Recipients: s.Recipients, Round: s.Round, RoundUnits: s.RoundUnits, RoundUnitMicros: s.RoundUnitMicros,
		RoundUnitCostMicros: s.RoundUnitCostMicros, RoundSettled: !s.RoundOpen(), LeaseUntil: s.LeaseUntil, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func nonNilTags(tags map[string]string) map[string]string {
	if tags == nil {
		return map[string]string{}
	}
	return tags
}

func sendOf(r schema.EmailAPISend) (emailapi.Send, error) {
	tags := map[string]string{}
	if len(r.Tags) > 0 {
		if err := json.Unmarshal(r.Tags, &tags); err != nil {
			return emailapi.Send{}, fmt.Errorf("emailapi: stored tags of send %s are unreadable: %w", r.ID, err)
		}
	}
	return emailapi.Send{
		ID: r.ID, WorkspaceID: r.WorkspaceID, IdempotencyKey: r.IdempotencyKey, Fingerprint: r.Fingerprint, Origin: emailapi.Origin(r.Origin),
		APIKeyID: string(r.APIKeyID), SenderID: r.SenderID, From: emailaddress.Address(r.FromAddress), Subject: r.Subject, Tags: tags,
		Recipients: r.Recipients, Round: r.Round, RoundUnits: r.RoundUnits, RoundUnitMicros: r.RoundUnitMicros, RoundUnitCostMicros: r.RoundUnitCostMicros,
		RoundSettled: r.RoundSettled, LeaseUntil: r.LeaseUntil, CreatedAt: r.CreatedAt,
	}, nil
}

func messageRow(m emailapi.Message) schema.EmailAPIMessage {
	updated := m.UpdatedAt
	if updated.IsZero() {
		updated = m.CreatedAt
	}
	return schema.EmailAPIMessage{
		ID: m.ID, SendID: m.SendID, Position: m.Position, WorkspaceID: m.WorkspaceID, Role: string(m.Role), Address: string(m.Address),
		Status: string(m.Status), ErrorCode: m.ErrorCode, MessageID: schema.OptionalText(m.MessageID), Round: m.Round,
		LastEvent: m.LastEvent, LastEventAt: m.LastEventAt, CreatedAt: m.CreatedAt, UpdatedAt: updated,
	}
}

func messageOf(r schema.EmailAPIMessage) emailapi.Message {
	return emailapi.Message{
		ID: r.ID, SendID: r.SendID, WorkspaceID: r.WorkspaceID, Position: r.Position, Role: emailapi.Role(r.Role),
		Address: emailaddress.Address(r.Address), Status: emailapi.MessageStatus(r.Status), ErrorCode: r.ErrorCode,
		MessageID: string(r.MessageID), Round: r.Round, LastEvent: r.LastEvent, LastEventAt: r.LastEventAt,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return emailapi.ErrMessageNotFound
	}
	return err
}
