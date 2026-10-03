package advertising_repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	ads "vozko/domain/advertising"
)

type wabaDirectory struct {
	db *gorm.DB
}

func NewWABADirectory(db *gorm.DB) *wabaDirectory {
	return &wabaDirectory{db: db}
}

func (d *wabaDirectory) WABAOf(ctx context.Context, workspaceID, businessPhoneID string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(businessPhoneID) == "" {
		return "", ads.ErrBusinessPhoneNotFound
	}
	var wabaIDs []string
	err := d.db.WithContext(ctx).Raw(
		`SELECT waba_id FROM whatsapp_business_phone_numbers WHERE id = ? AND owner_workspace_id = ? AND deleted_at IS NULL`,
		businessPhoneID, workspaceID,
	).Scan(&wabaIDs).Error
	if err != nil {
		return "", err
	}
	if len(wabaIDs) != 1 || strings.TrimSpace(wabaIDs[0]) == "" {
		return "", ads.ErrBusinessPhoneNotFound
	}
	return wabaIDs[0], nil
}
