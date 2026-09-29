package sip_trunk_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/sip_trunk"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

const lastErrorLimit = 500

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) sip_trunk.Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, trunk *sip_trunk.SIPTrunk) error {
	record, err := toSchema(trunk)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	trunk.ID = record.ID
	trunk.RegistrationStatus = sip_trunk.RegistrationStatus(record.RegistrationStatus)
	trunk.CreatedAt, trunk.UpdatedAt = record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *repository) Update(ctx context.Context, trunk *sip_trunk.SIPTrunk) error {
	record, err := toSchema(trunk)
	if err != nil {
		return err
	}
	changes := map[string]any{
		"name":       record.Name,
		"trunk_type": record.TrunkType,
		"host":       record.Host,
		"port":       record.Port,
		"domain":     record.Domain,
		"transport":  record.Transport,
		"username":   record.Username,
		"enabled":    record.Enabled,
		"settings":   record.Settings,
		"updated_at": time.Now(),
	}
	if trunk.Password != "" {
		changes["password"] = record.Password
	}
	result := r.db.WithContext(ctx).Model(&schema.SIPTrunk{}).
		Where("id = ? AND workspace_id = ?", trunk.ID, trunk.WorkspaceID).
		Updates(changes)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sip_trunk.ErrTrunkNotFound
	}
	return nil
}

func (r *repository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.SIPTrunk{}, "id = ? AND workspace_id = ?", id, workspaceID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sip_trunk.ErrTrunkNotFound
	}
	return nil
}

func (r *repository) FindInWorkspace(ctx context.Context, workspaceID, id string) (*sip_trunk.SIPTrunk, error) {
	var record schema.SIPTrunk
	err := r.db.WithContext(ctx).First(&record, "id = ? AND workspace_id = ?", id, workspaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, sip_trunk.ErrTrunkNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(&record)
}

func (r *repository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*sip_trunk.SIPTrunk, error) {
	var records []schema.SIPTrunk
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return toDomainList(records)
}

func (r *repository) FindEnabled(ctx context.Context) ([]*sip_trunk.SIPTrunk, error) {
	var records []schema.SIPTrunk
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return toDomainList(records)
}

func (r *repository) UpdateStatus(ctx context.Context, id string, status sip_trunk.RegistrationStatus, lastError string) error {
	if len(lastError) > lastErrorLimit {
		lastError = lastError[:lastErrorLimit]
	}
	return r.db.WithContext(ctx).Model(&schema.SIPTrunk{}).Where("id = ?", id).Updates(map[string]any{
		"registration_status": string(status),
		"last_error":          lastError,
		"updated_at":          time.Now(),
	}).Error
}

func toSchema(trunk *sip_trunk.SIPTrunk) (*schema.SIPTrunk, error) {
	settings, err := json.Marshal(trunk.Settings)
	if err != nil {
		return nil, fmt.Errorf("encode trunk settings: %w", err)
	}
	status := trunk.RegistrationStatus
	if status == "" {
		status = sip_trunk.RegistrationStatusUnregistered
	}
	record := &schema.SIPTrunk{
		ID:                 trunk.ID,
		WorkspaceID:        trunk.WorkspaceID,
		Name:               trunk.Name,
		TrunkType:          string(trunk.TrunkType),
		Host:               trunk.Host,
		Port:               trunk.Port,
		Domain:             trunk.Domain,
		Transport:          string(trunk.Transport),
		Username:           trunk.Username,
		Enabled:            trunk.Enabled,
		Settings:           datatypes.JSON(settings),
		RegistrationStatus: string(status),
		LastError:          trunk.LastError,
	}
	if trunk.Password != "" {
		record.Password = piigorm.NewEncrypted(trunk.Password)
	}
	return record, nil
}

func toDomain(record *schema.SIPTrunk) (*sip_trunk.SIPTrunk, error) {
	var settings sip_trunk.Settings
	if len(record.Settings) > 0 {
		if err := json.Unmarshal(record.Settings, &settings); err != nil {
			return nil, fmt.Errorf("decode settings of trunk %s: %w", record.ID, err)
		}
	}
	return &sip_trunk.SIPTrunk{
		ID:                 record.ID,
		WorkspaceID:        record.WorkspaceID,
		Name:               record.Name,
		TrunkType:          sip_trunk.TrunkType(record.TrunkType),
		Host:               record.Host,
		Port:               record.Port,
		Domain:             record.Domain,
		Transport:          sip_trunk.Transport(record.Transport),
		Username:           record.Username,
		Password:           record.Password.String(),
		Enabled:            record.Enabled,
		Settings:           settings,
		RegistrationStatus: sip_trunk.RegistrationStatus(record.RegistrationStatus),
		LastError:          record.LastError,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
	}, nil
}

func toDomainList(records []schema.SIPTrunk) ([]*sip_trunk.SIPTrunk, error) {
	trunks := make([]*sip_trunk.SIPTrunk, 0, len(records))
	for i := range records {
		trunk, err := toDomain(&records[i])
		if err != nil {
			return nil, err
		}
		trunks = append(trunks, trunk)
	}
	return trunks, nil
}
