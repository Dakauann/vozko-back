package emailwebhook_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailapi"
	wh "vozko/domain/emailwebhook"
	"vozko/infra/database/schema"
)

type Deliveries struct {
	db *gorm.DB
}

var _ wh.DeliveryStore = (*Deliveries)(nil)

func NewDeliveries(db *gorm.DB) *Deliveries {
	return &Deliveries{db: db}
}

const insertDeliveryColumns = "(id, endpoint_id, workspace_id, event_id, event_type, message_id, payload, status, attempts, last_status_code, last_error, created_at, updated_at)"

func InsertSQL(n int) string {
	row := "(?, ?, ?, ?, ?, ?, ?, ?, 0, 0, '', ?, ?)"
	rows := make([]string, n)
	for i := range rows {
		rows[i] = row
	}
	return "INSERT INTO email_webhook_deliveries " + insertDeliveryColumns + " VALUES " + strings.Join(rows, ", ") +
		" ON CONFLICT (endpoint_id, event_id) DO NOTHING RETURNING id"
}

func (s *Deliveries) InsertBatch(ctx context.Context, deliveries []wh.Delivery) ([]wh.Delivery, error) {
	if len(deliveries) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(deliveries)*10)
	for _, d := range deliveries {
		args = append(args, d.ID, d.EndpointID, d.WorkspaceID, d.EventID, string(d.EventType), d.MessageID, string(d.Payload), string(wh.StatusQueued), d.CreatedAt, d.CreatedAt)
	}
	var ids []string
	if err := s.db.WithContext(ctx).Raw(InsertSQL(len(deliveries)), args...).Scan(&ids).Error; err != nil {
		return nil, err
	}
	inserted := make(map[string]bool, len(ids))
	for _, id := range ids {
		inserted[id] = true
	}
	out := make([]wh.Delivery, 0, len(ids))
	for _, d := range deliveries {
		if inserted[d.ID] {
			d.Status = wh.StatusQueued
			out = append(out, d)
		}
	}
	return out, nil
}

type targetRow struct {
	schema.EmailWebhookDelivery
	EndpointURL          string
	EndpointEnabled      bool
	EndpointSecretCipher []byte
	EndpointFailingSince *time.Time
	EndpointAlertedAt    *time.Time
}

func (s *Deliveries) Target(ctx context.Context, id string) (*wh.Target, error) {
	var rows []targetRow
	err := s.db.WithContext(ctx).Raw(`SELECT d.*, e.url AS endpoint_url, e.enabled AS endpoint_enabled, e.secret_cipher AS endpoint_secret_cipher,
		e.failing_since AS endpoint_failing_since, e.alerted_at AS endpoint_alerted_at
		FROM email_webhook_deliveries d JOIN email_webhook_endpoints e ON e.id = d.endpoint_id WHERE d.id = ?`, id).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, wh.ErrDeliveryNotFound
	}
	r := rows[0]
	return &wh.Target{
		Delivery: deliveryOf(r.EmailWebhookDelivery),
		Endpoint: wh.Endpoint{
			ID: r.EndpointID, WorkspaceID: r.WorkspaceID, URL: r.EndpointURL, Enabled: r.EndpointEnabled, SecretCipher: r.EndpointSecretCipher,
			FailingSince: r.EndpointFailingSince, AlertedAt: r.EndpointAlertedAt,
		},
	}, nil
}

func (s *Deliveries) Claim(ctx context.Context, id string, attempt int, at time.Time) (bool, error) {
	res := s.db.WithContext(ctx).Model(&schema.EmailWebhookDelivery{}).
		Where("id = ? AND status = ? AND attempts = ?", id, string(wh.StatusQueued), attempt-1).
		Updates(map[string]any{"status": string(wh.StatusSending), "attempts": attempt, "updated_at": at})
	return res.RowsAffected == 1, res.Error
}

func (s *Deliveries) Finish(ctx context.Context, f wh.Finish) error {
	updates := map[string]any{
		"status": string(f.Outcome.Status), "next_attempt_at": f.Outcome.NextAttemptAt, "last_status_code": f.StatusCode,
		"last_error": wh.ClipError(f.Error), "updated_at": f.At,
	}
	if f.Outcome.Status == wh.StatusDelivered {
		updates["delivered_at"] = f.At
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&schema.EmailWebhookDelivery{}).Where("id = ? AND status = ?", f.ID, string(wh.StatusSending)).Updates(updates).Error; err != nil {
			return err
		}
		a := f.Attempt
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schema.EmailWebhookAttempt{
			DeliveryID: f.ID, Attempt: a.Attempt, StatusCode: a.StatusCode, DurationMS: a.DurationMS, Error: wh.ClipError(a.Error),
			NextAttemptAt: a.NextAttemptAt, AttemptedAt: a.AttemptedAt,
		}).Error
	})
}

func (s *Deliveries) Skip(ctx context.Context, id string, reason string, at time.Time) error {
	return s.db.WithContext(ctx).Model(&schema.EmailWebhookDelivery{}).
		Where("id = ? AND status IN ?", id, []string{string(wh.StatusQueued), string(wh.StatusSending)}).
		Updates(map[string]any{"status": string(wh.StatusSkipped), "last_error": wh.ClipError(reason), "updated_at": at}).Error
}

const requeueSQL = `UPDATE email_webhook_deliveries d SET status = 'queued', updated_at = ?
	FROM (
		(SELECT id FROM email_webhook_deliveries WHERE status = 'retrying' AND next_attempt_at <= ? ORDER BY next_attempt_at LIMIT ?)
		UNION
		(SELECT id FROM email_webhook_deliveries WHERE (status = 'queued' OR status = 'sending') AND updated_at < ?
			AND (status = 'sending' OR updated_at < ?) ORDER BY updated_at LIMIT ?)
	) due
	WHERE d.id = due.id AND ((d.status = 'retrying' AND d.next_attempt_at <= ?)
		OR ((d.status = 'queued' OR d.status = 'sending') AND d.updated_at < ? AND (d.status = 'sending' OR d.updated_at < ?)))
	RETURNING d.id, d.endpoint_id, d.attempts`

func (s *Deliveries) Requeue(ctx context.Context, now time.Time, limit int) ([]wh.Requeued, error) {
	var out []wh.Requeued
	err := s.db.WithContext(ctx).Raw(requeueSQL, now, now, limit, now.Add(-wh.SendingStaleAfter), now.Add(-wh.QueuedStaleAfter), limit,
		now, now.Add(-wh.SendingStaleAfter), now.Add(-wh.QueuedStaleAfter)).Scan(&out).Error
	return out, err
}

func (s *Deliveries) Retry(ctx context.Context, workspaceID, endpointID, id string, at time.Time) (*wh.Delivery, error) {
	var row schema.EmailWebhookDelivery
	if err := s.db.WithContext(ctx).Where("id = ? AND endpoint_id = ? AND workspace_id = ?", id, endpointID, workspaceID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wh.ErrDeliveryNotFound
		}
		return nil, err
	}
	if !deliveryOf(row).Retryable() {
		return nil, wh.ErrDeliveryNotRetryable
	}
	res := s.db.WithContext(ctx).Model(&schema.EmailWebhookDelivery{}).
		Where("id = ? AND status IN ?", id, []string{string(wh.StatusFailed), string(wh.StatusSkipped)}).
		Updates(map[string]any{"status": string(wh.StatusQueued), "next_attempt_at": nil, "updated_at": at})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, wh.ErrDeliveryNotRetryable
	}
	d := deliveryOf(row)
	d.Status = wh.StatusQueued
	return &d, nil
}

func (s *Deliveries) ListByEndpoint(ctx context.Context, workspaceID, endpointID string, cursor emailapi.Cursor, limit int) ([]wh.Delivery, error) {
	q := s.db.WithContext(ctx).Where("endpoint_id = ? AND workspace_id = ?", endpointID, workspaceID)
	if !cursor.Empty() {
		q = q.Where("(created_at, id) < (?, ?)", cursor.CreatedAt, cursor.ID)
	}
	var rows []schema.EmailWebhookDelivery
	if err := q.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]wh.Delivery, len(rows))
	for i, r := range rows {
		out[i] = deliveryOf(r)
	}
	return out, nil
}

func (s *Deliveries) Attempts(ctx context.Context, deliveryIDs []string) (map[string][]wh.Attempt, error) {
	out := map[string][]wh.Attempt{}
	if len(deliveryIDs) == 0 {
		return out, nil
	}
	var rows []schema.EmailWebhookAttempt
	if err := s.db.WithContext(ctx).Where("delivery_id IN ?", deliveryIDs).Order("delivery_id, attempt").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DeliveryID] = append(out[r.DeliveryID], wh.Attempt{
			DeliveryID: r.DeliveryID, Attempt: r.Attempt, StatusCode: r.StatusCode, DurationMS: r.DurationMS, Error: r.Error,
			NextAttemptAt: r.NextAttemptAt, AttemptedAt: r.AttemptedAt,
		})
	}
	return out, nil
}

func (s *Deliveries) PurgeAttempts(ctx context.Context, before time.Time, limit int) (int64, error) {
	res := s.db.WithContext(ctx).Exec(`DELETE FROM email_webhook_attempts WHERE (delivery_id, attempt) IN
		(SELECT delivery_id, attempt FROM email_webhook_attempts WHERE attempted_at < ? LIMIT ?)`, before, limit)
	return res.RowsAffected, res.Error
}

func (s *Deliveries) PurgeDeliveries(ctx context.Context, before time.Time, limit int) (int64, error) {
	res := s.db.WithContext(ctx).Exec(`DELETE FROM email_webhook_deliveries WHERE id IN
		(SELECT id FROM email_webhook_deliveries WHERE created_at < ? AND (status = 'delivered' OR status = 'failed' OR status = 'skipped') LIMIT ?)`, before, limit)
	return res.RowsAffected, res.Error
}

func deliveryOf(r schema.EmailWebhookDelivery) wh.Delivery {
	return wh.Delivery{
		ID: r.ID, EndpointID: r.EndpointID, WorkspaceID: r.WorkspaceID, EventID: r.EventID, EventType: wh.EventType(r.EventType),
		MessageID: r.MessageID, Payload: []byte(r.Payload), Status: wh.DeliveryStatus(r.Status), Attempts: r.Attempts,
		NextAttemptAt: r.NextAttemptAt, LastStatusCode: r.LastStatusCode, LastError: r.LastError, CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, DeliveredAt: r.DeliveredAt,
	}
}
