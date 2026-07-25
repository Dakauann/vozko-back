package lead_message_window

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	lmw "vozko/domain/lead_message_window"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) lmw.Repository {
	return &repository{db: db}
}

func (r *repository) RecordMessage(leadID, businessPhoneID string) (*lmw.LeadMessageWindow, error) {
	leadID = strings.TrimSpace(leadID)
	businessPhoneID = strings.TrimSpace(businessPhoneID)

	if leadID == "" {
		return nil, lmw.ErrLeadIDRequired
	}
	if businessPhoneID == "" {
		return nil, lmw.ErrBusinessPhoneIDRequied
	}

	now := time.Now().UTC()

	window := schema.LeadMessageWindow{
		ID:              uuid.New().String(),
		LeadID:          leadID,
		BusinessPhoneID: businessPhoneID,
		LastMessageAt:   now,
	}

	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "lead_id"}, {Name: "business_phone_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_message_at", "updated_at"}),
	}).Create(&window).Error

	if err != nil {
		return nil, err
	}

	return toDomain(&window), nil
}

func (r *repository) FindByLeadAndBusinessPhone(leadID, businessPhoneID string) (*lmw.LeadMessageWindow, error) {
	leadID = strings.TrimSpace(leadID)
	businessPhoneID = strings.TrimSpace(businessPhoneID)

	if leadID == "" {
		return nil, lmw.ErrLeadIDRequired
	}
	if businessPhoneID == "" {
		return nil, lmw.ErrBusinessPhoneIDRequied
	}

	var window schema.LeadMessageWindow
	err := r.db.Where("lead_id = ? AND business_phone_id = ?", leadID, businessPhoneID).First(&window).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, lmw.ErrWindowNotFound
		}
		return nil, err
	}

	return toDomain(&window), nil
}

func (r *repository) FindAllByLead(leadID string) ([]*lmw.LeadMessageWindow, error) {
	leadID = strings.TrimSpace(leadID)
	if leadID == "" {
		return nil, lmw.ErrLeadIDRequired
	}

	var windows []schema.LeadMessageWindow
	if err := r.db.Where("lead_id = ?", leadID).Find(&windows).Error; err != nil {
		return nil, err
	}

	result := make([]*lmw.LeadMessageWindow, len(windows))
	for i, w := range windows {
		result[i] = toDomain(&w)
	}

	return result, nil
}

func (r *repository) FindOpenWindowsByLeadIDs(leadIDs []string, forBusinessPhoneID string) (map[string]*lmw.LeadMessageWindow, error) {
	if len(leadIDs) == 0 {
		return make(map[string]*lmw.LeadMessageWindow), nil
	}

	forBusinessPhoneID = strings.TrimSpace(forBusinessPhoneID)
	if forBusinessPhoneID == "" {
		return nil, lmw.ErrBusinessPhoneIDRequied
	}

	windowCutoff := time.Now().UTC().Add(-lmw.MessageWindowDuration)

	var windows []schema.LeadMessageWindow
	err := r.db.Where("lead_id IN ? AND business_phone_id = ? AND last_message_at > ?",
		leadIDs, forBusinessPhoneID, windowCutoff).Find(&windows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]*lmw.LeadMessageWindow)
	for _, w := range windows {
		result[w.LeadID] = toDomain(&w)
	}

	return result, nil
}

func (r *repository) IsWindowOpen(leadID, businessPhoneID string) (bool, error) {
	window, err := r.FindByLeadAndBusinessPhone(leadID, businessPhoneID)
	if err != nil {
		if errors.Is(err, lmw.ErrWindowNotFound) {
			return false, nil
		}
		return false, err
	}

	return window.IsWindowOpen(), nil
}

func (r *repository) GetLastMessageTime(leadID, businessPhoneID string) (*time.Time, error) {
	window, err := r.FindByLeadAndBusinessPhone(leadID, businessPhoneID)
	if err != nil {
		if errors.Is(err, lmw.ErrWindowNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &window.LastMessageAt, nil
}

func toDomain(s *schema.LeadMessageWindow) *lmw.LeadMessageWindow {
	return &lmw.LeadMessageWindow{
		ID:              s.ID,
		LeadID:          s.LeadID,
		BusinessPhoneID: s.BusinessPhoneID,
		LastMessageAt:   s.LastMessageAt,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}
