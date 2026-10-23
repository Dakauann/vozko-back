package emailwebhook_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	wh "vozko/domain/emailwebhook"
	"vozko/infra/database/schema"
)

type Endpoints struct {
	db *gorm.DB
}

var _ wh.EndpointStore = (*Endpoints)(nil)

func NewEndpoints(db *gorm.DB) *Endpoints {
	return &Endpoints{db: db}
}

func (s *Endpoints) Create(ctx context.Context, e *wh.Endpoint, limit int) error {
	row, err := endpointRow(*e)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "email_webhook_endpoints:"+e.WorkspaceID).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&schema.EmailWebhookEndpoint{}).Where("workspace_id = ?", e.WorkspaceID).Count(&n).Error; err != nil {
			return err
		}
		if n >= int64(limit) {
			return wh.ErrLimitReached
		}
		return tx.Create(&row).Error
	})
}

func (s *Endpoints) List(ctx context.Context, workspaceID string) ([]wh.Endpoint, error) {
	var rows []schema.EmailWebhookEndpoint
	if err := s.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at").Find(&rows).Error; err != nil {
		return nil, err
	}
	return endpoints(rows)
}

func (s *Endpoints) Get(ctx context.Context, workspaceID, id string) (*wh.Endpoint, error) {
	var row schema.EmailWebhookEndpoint
	if err := s.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wh.ErrEndpointNotFound
		}
		return nil, err
	}
	e, err := endpointOf(row)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Endpoints) Update(ctx context.Context, e *wh.Endpoint) error {
	events, err := json.Marshal(e.Events)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&schema.EmailWebhookEndpoint{}).Where("workspace_id = ? AND id = ?", e.WorkspaceID, e.ID).
		Updates(map[string]any{"url": e.URL, "events": datatypes.JSON(events), "enabled": e.Enabled, "updated_at": e.UpdatedAt})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return wh.ErrEndpointNotFound
	}
	return nil
}

func (s *Endpoints) SetSecret(ctx context.Context, workspaceID, id string, cipher []byte, hint string, at time.Time) error {
	res := s.db.WithContext(ctx).Model(&schema.EmailWebhookEndpoint{}).Where("workspace_id = ? AND id = ?", workspaceID, id).
		Updates(map[string]any{"secret_cipher": cipher, "secret_hint": hint, "updated_at": at})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return wh.ErrEndpointNotFound
	}
	return nil
}

func (s *Endpoints) Delete(ctx context.Context, workspaceID, id string) error {
	res := s.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Delete(&schema.EmailWebhookEndpoint{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return wh.ErrEndpointNotFound
	}
	return nil
}

func (s *Endpoints) Subscribed(ctx context.Context, workspaceID string, t wh.EventType) ([]wh.Endpoint, error) {
	wanted, err := json.Marshal([]wh.EventType{t})
	if err != nil {
		return nil, err
	}
	var rows []schema.EmailWebhookEndpoint
	if err := s.db.WithContext(ctx).Where("workspace_id = ? AND enabled AND events @> ?::jsonb", workspaceID, string(wanted)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return endpoints(rows)
}

func (s *Endpoints) MarkSuccess(ctx context.Context, id string, at time.Time) error {
	return s.db.WithContext(ctx).Model(&schema.EmailWebhookEndpoint{}).Where("id = ?", id).
		Updates(map[string]any{"last_success_at": at, "failing_since": nil, "alerted_at": nil}).Error
}

func (s *Endpoints) MarkFailure(ctx context.Context, id string, at time.Time) error {
	return s.db.WithContext(ctx).Exec("UPDATE email_webhook_endpoints SET last_failure_at = ?, failing_since = COALESCE(failing_since, ?) WHERE id = ?", at, at, id).Error
}

func (s *Endpoints) MarkAlerted(ctx context.Context, id string, at time.Time) error {
	return s.db.WithContext(ctx).Model(&schema.EmailWebhookEndpoint{}).Where("id = ?", id).Update("alerted_at", at).Error
}

func endpoints(rows []schema.EmailWebhookEndpoint) ([]wh.Endpoint, error) {
	out := make([]wh.Endpoint, 0, len(rows))
	for _, r := range rows {
		e, err := endpointOf(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func endpointRow(e wh.Endpoint) (schema.EmailWebhookEndpoint, error) {
	events, err := json.Marshal(e.Events)
	if err != nil {
		return schema.EmailWebhookEndpoint{}, err
	}
	return schema.EmailWebhookEndpoint{
		ID: e.ID, WorkspaceID: e.WorkspaceID, URL: e.URL, Events: datatypes.JSON(events), Enabled: e.Enabled,
		SecretCipher: e.SecretCipher, SecretHint: e.SecretHint, FailingSince: e.FailingSince, AlertedAt: e.AlertedAt,
		LastSuccessAt: e.LastSuccessAt, LastFailureAt: e.LastFailureAt, CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}, nil
}

func endpointOf(r schema.EmailWebhookEndpoint) (wh.Endpoint, error) {
	var events []wh.EventType
	if err := json.Unmarshal(r.Events, &events); err != nil {
		return wh.Endpoint{}, fmt.Errorf("emailwebhook: stored events of %s are unreadable: %w", r.ID, err)
	}
	for _, e := range events {
		if !e.Valid() {
			return wh.Endpoint{}, fmt.Errorf("emailwebhook: endpoint %s stores the unknown event %q", r.ID, e)
		}
	}
	return wh.Endpoint{
		ID: r.ID, WorkspaceID: r.WorkspaceID, URL: r.URL, Events: events, Enabled: r.Enabled, SecretCipher: r.SecretCipher,
		SecretHint: r.SecretHint, FailingSince: r.FailingSince, AlertedAt: r.AlertedAt, LastSuccessAt: r.LastSuccessAt,
		LastFailureAt: r.LastFailureAt, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}
