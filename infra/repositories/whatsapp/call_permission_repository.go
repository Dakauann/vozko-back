package whatsapp_repository

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/lead"
	callpermission "vozko/domain/whatsapp/call_permission"
	"vozko/infra/database/schema"
)

func canonicalNumber(n string) string {
	return lead.NormalizeWhatsAppNumber(callpermission.NormalizeNumber(n))
}

func leadIDPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type callPermissionRepository struct {
	db *gorm.DB
}

func NewCallPermissionRepository(db *gorm.DB) callpermission.Repository {
	return &callPermissionRepository{db: db}
}

func (r *callPermissionRepository) Upsert(p *callpermission.CallPermission) error {
	if p == nil {
		return errors.New("call permission is nil")
	}
	workspaceID := strings.TrimSpace(p.WorkspaceID)
	businessPhoneID := strings.TrimSpace(p.BusinessPhoneID)
	userNumber := canonicalNumber(p.UserNumber)
	if workspaceID == "" {

		return errors.New("workspace id is required")
	}
	if businessPhoneID == "" || userNumber == "" {
		return errors.New("business phone id and user number are required")
	}

	now := time.Now().UTC()
	record := schema.WhatsAppCallPermission{
		ID:              uuid.New().String(),
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		UserNumber:      userNumber,
		LeadID:          leadIDPtr(p.LeadID),
		Status:          string(p.Status),
		ExpiresAt:       p.ExpiresAt,
		ResponseSource:  p.ResponseSource,
		RequestedAt:     p.RequestedAt,
		RespondedAt:     p.RespondedAt,
		UpdatedAt:       now,
	}

	assignments := map[string]any{
		"workspace_id":    workspaceID,
		"status":          record.Status,
		"expires_at":      record.ExpiresAt,
		"response_source": record.ResponseSource,
		"updated_at":      now,
	}
	if record.LeadID != nil {
		assignments["lead_id"] = record.LeadID
	}
	if record.RequestedAt != nil {
		assignments["requested_at"] = record.RequestedAt
	}
	if record.RespondedAt != nil {
		assignments["responded_at"] = record.RespondedAt
	}

	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "business_phone_id"}, {Name: "user_number"}},
		DoUpdates: clause.Assignments(assignments),
	}).Create(&record).Error
}

func (r *callPermissionRepository) FindByPhoneAndNumber(businessPhoneID, userNumber string) (*callpermission.CallPermission, error) {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	userNumber = canonicalNumber(userNumber)
	if businessPhoneID == "" || userNumber == "" {
		return nil, nil
	}
	var record schema.WhatsAppCallPermission
	err := r.db.Where("business_phone_id = ? AND user_number = ?", businessPhoneID, userNumber).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toCallPermissionDomain(&record), nil
}

func (r *callPermissionRepository) FindByPhoneAndLead(businessPhoneID, leadID string) (*callpermission.CallPermission, error) {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	leadID = strings.TrimSpace(leadID)
	if businessPhoneID == "" || leadID == "" {
		return nil, nil
	}
	var record schema.WhatsAppCallPermission
	err := r.db.Where("business_phone_id = ? AND lead_id = ?", businessPhoneID, leadID).
		Order("updated_at DESC").First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toCallPermissionDomain(&record), nil
}

func toCallPermissionDomain(s *schema.WhatsAppCallPermission) *callpermission.CallPermission {
	return &callpermission.CallPermission{
		ID:              s.ID,
		WorkspaceID:     s.WorkspaceID,
		BusinessPhoneID: s.BusinessPhoneID,
		LeadID:          derefStr(s.LeadID),
		UserNumber:      s.UserNumber,
		Status:          callpermission.Status(s.Status),
		ExpiresAt:       s.ExpiresAt,
		ResponseSource:  s.ResponseSource,
		RequestedAt:     s.RequestedAt,
		RespondedAt:     s.RespondedAt,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}
