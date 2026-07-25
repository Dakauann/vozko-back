package sip_trunk_repository

import (
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/sip_trunk"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) sip_trunk.Repository {
	return &repository{db: db}
}

func (r *repository) Create(trunk *sip_trunk.SIPTrunk) error {
	return r.db.Create(toSchema(trunk)).Error
}

func (r *repository) Update(trunk *sip_trunk.SIPTrunk) error {
	return r.db.Save(toSchema(trunk)).Error
}

func (r *repository) Delete(id string) error {
	return r.db.Delete(&schema.SIPTrunk{}, "id = ?", id).Error
}

func (r *repository) FindByID(id string) (*sip_trunk.SIPTrunk, error) {
	var s schema.SIPTrunk
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, sip_trunk.ErrTrunkNotFound
		}
		return nil, err
	}
	return toDomain(&s), nil
}

func (r *repository) FindByIDs(ids []string) ([]*sip_trunk.SIPTrunk, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var trunks []schema.SIPTrunk
	if err := r.db.Where("id IN ?", ids).Order("created_at DESC").Find(&trunks).Error; err != nil {
		return nil, err
	}
	result := make([]*sip_trunk.SIPTrunk, len(trunks))
	for i, t := range trunks {
		result[i] = toDomain(&t)
	}
	return result, nil
}

func (r *repository) FindAll(page, pageSize int) ([]*sip_trunk.SIPTrunk, int64, error) {
	var trunks []schema.SIPTrunk
	var total int64

	query := r.db.Model(&schema.SIPTrunk{})

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		query = query.Offset(offset).Limit(pageSize)
	}

	if err := query.Order("created_at DESC").Find(&trunks).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*sip_trunk.SIPTrunk, len(trunks))
	for i, t := range trunks {
		result[i] = toDomain(&t)
	}

	return result, total, nil
}

func (r *repository) FindAccessible(workspaceID string, extraTrunkIDs []string, page, pageSize int) ([]*sip_trunk.SIPTrunk, int64, error) {
	var trunks []schema.SIPTrunk
	var total int64

	query := r.db.Model(&schema.SIPTrunk{})

	switch {
	case workspaceID != "" && len(extraTrunkIDs) > 0:
		query = query.Where("workspace_id = ? OR is_globally_visible = ? OR id IN ?", workspaceID, true, extraTrunkIDs)
	case workspaceID != "":
		query = query.Where("workspace_id = ? OR is_globally_visible = ?", workspaceID, true)
	case len(extraTrunkIDs) > 0:
		query = query.Where("is_globally_visible = ? OR id IN ?", true, extraTrunkIDs)
	default:
		query = query.Where("is_globally_visible = ?", true)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page > 0 && pageSize > 0 {
		offset := (page - 1) * pageSize
		query = query.Offset(offset).Limit(pageSize)
	}

	if err := query.Order("created_at DESC").Find(&trunks).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*sip_trunk.SIPTrunk, len(trunks))
	for i, t := range trunks {
		result[i] = toDomain(&t)
	}
	return result, total, nil
}

func (r *repository) CountByWorkspace(workspaceID string) (int64, error) {
	var total int64
	if err := r.db.Model(&schema.SIPTrunk{}).
		Where("workspace_id = ?", workspaceID).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func (r *repository) FindEnabled() ([]*sip_trunk.SIPTrunk, error) {
	var trunks []schema.SIPTrunk
	if err := r.db.Where("enabled = ?", true).Find(&trunks).Error; err != nil {
		return nil, err
	}

	result := make([]*sip_trunk.SIPTrunk, len(trunks))
	for i, t := range trunks {
		result[i] = toDomain(&t)
	}

	return result, nil
}

func (r *repository) UpdateStatus(id string, status sip_trunk.RegistrationStatus, lastError string) error {
	return r.db.Model(&schema.SIPTrunk{}).Where("id = ?", id).Updates(map[string]interface{}{
		"registration_status": string(status),
		"last_error":          lastError,
	}).Error
}

// emptyToNilUUID maps an empty string to nil so a uuid column stores NULL
// instead of ” (which Postgres rejects: invalid input syntax for type uuid).
func emptyToNilUUID(s string) *string {
	if strings.TrimSpace(s) == "" {
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

func toSchema(t *sip_trunk.SIPTrunk) *schema.SIPTrunk {
	s := &schema.SIPTrunk{
		ID:                 t.ID,
		WorkspaceID:        t.WorkspaceID,
		OwnerAssignedBy:    emptyToNilUUID(t.OwnerAssignedBy),
		OwnerAssignedAt:    t.OwnerAssignedAt,
		Name:               t.Name,
		Description:        t.Description,
		Host:               t.Host,
		Port:               t.Port,
		Username:           t.Username,
		Password:           piigorm.NewEncrypted(t.Password),
		Domain:             t.Domain,
		Transport:          string(t.Transport),
		TrunkType:          string(t.TrunkType),
		IsRotational:       t.IsRotational,
		PhoneNumber:        t.PhoneNumber,
		Enabled:            t.Enabled,
		IsGloballyVisible:  t.IsGloballyVisible,
		RegistrationStatus: string(t.RegistrationStatus),
		LastError:          t.LastError,
		CreatedAt:          t.CreatedAt,
		UpdatedAt:          t.UpdatedAt,
	}

	s.OutboundProxy = t.OutboundProxy
	s.AuthUsername = t.AuthUsername
	s.UserAgent = t.UserAgent

	s.RegisterEnabled = t.RegisterEnabled
	s.RegisterExpirySeconds = t.RegisterExpirySeconds
	s.RegisterRetrySeconds = t.RegisterRetrySeconds
	s.SessionTimerEnabled = t.SessionTimerEnabled
	s.SessionTimerSeconds = t.SessionTimerSeconds
	s.MinSESeconds = t.MinSESeconds
	if t.SessionRefresher != nil {
		v := string(*t.SessionRefresher)
		s.SessionRefresher = &v
	}

	s.BindHost = t.BindHost
	s.BindPort = t.BindPort
	s.PublicAddress = t.PublicAddress
	s.StunServers = marshalStringSlice(t.StunServers)
	s.StunEnabled = t.StunEnabled

	s.Codecs = marshalCodecSlice(t.Codecs)
	s.PTimeMs = t.PTimeMs
	s.DTMFPayloadType = t.DTMFPayloadType
	if t.SRTPMode != nil {
		v := string(*t.SRTPMode)
		s.SRTPMode = &v
	}

	s.DialPrefix = t.DialPrefix
	s.DialStripDigits = t.DialStripDigits
	if t.NumberFormat != nil {
		v := string(*t.NumberFormat)
		s.NumberFormat = &v
	}
	s.HideCallerID = t.HideCallerID

	s.RingingTimeoutSeconds = t.RingingTimeoutSeconds

	return s
}

func toDomain(s *schema.SIPTrunk) *sip_trunk.SIPTrunk {
	t := &sip_trunk.SIPTrunk{
		ID:                 s.ID,
		WorkspaceID:        s.WorkspaceID,
		OwnerAssignedBy:    derefStr(s.OwnerAssignedBy),
		OwnerAssignedAt:    s.OwnerAssignedAt,
		Name:               s.Name,
		Description:        s.Description,
		Host:               s.Host,
		Port:               s.Port,
		Username:           s.Username,
		Password:           s.Password.String(),
		Domain:             s.Domain,
		Transport:          sip_trunk.TransportType(s.Transport),
		TrunkType:          sip_trunk.TrunkType(s.TrunkType),
		IsRotational:       s.IsRotational,
		PhoneNumber:        s.PhoneNumber,
		Enabled:            s.Enabled,
		IsGloballyVisible:  s.IsGloballyVisible,
		RegistrationStatus: sip_trunk.RegistrationStatus(s.RegistrationStatus),
		LastError:          s.LastError,
		LastRegisteredAt:   s.LastRegisteredAt,
		CreatedAt:          s.CreatedAt,
		UpdatedAt:          s.UpdatedAt,
	}

	t.OutboundProxy = s.OutboundProxy
	t.AuthUsername = s.AuthUsername
	t.UserAgent = s.UserAgent

	t.RegisterEnabled = s.RegisterEnabled
	t.RegisterExpirySeconds = s.RegisterExpirySeconds
	t.RegisterRetrySeconds = s.RegisterRetrySeconds
	t.SessionTimerEnabled = s.SessionTimerEnabled
	t.SessionTimerSeconds = s.SessionTimerSeconds
	t.MinSESeconds = s.MinSESeconds
	if s.SessionRefresher != nil {
		v := sip_trunk.SessionRefresher(*s.SessionRefresher)
		t.SessionRefresher = &v
	}

	t.BindHost = s.BindHost
	t.BindPort = s.BindPort
	t.PublicAddress = s.PublicAddress
	t.StunServers = unmarshalStringSlice(s.StunServers)
	t.StunEnabled = s.StunEnabled

	t.Codecs = unmarshalCodecSlice(s.Codecs)
	t.PTimeMs = s.PTimeMs
	t.DTMFPayloadType = s.DTMFPayloadType
	if s.SRTPMode != nil {
		v := sip_trunk.SRTPMode(*s.SRTPMode)
		t.SRTPMode = &v
	}

	t.DialPrefix = s.DialPrefix
	t.DialStripDigits = s.DialStripDigits
	if s.NumberFormat != nil {
		v := sip_trunk.NumberFormat(*s.NumberFormat)
		t.NumberFormat = &v
	}
	t.HideCallerID = s.HideCallerID

	t.RingingTimeoutSeconds = s.RingingTimeoutSeconds

	return t
}

func marshalStringSlice(v []string) datatypes.JSON {
	if len(v) == 0 {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

func unmarshalStringSlice(b datatypes.JSON) []string {
	if len(b) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func marshalCodecSlice(v []sip_trunk.CodecID) datatypes.JSON {
	if len(v) == 0 {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return datatypes.JSON(b)
}

func unmarshalCodecSlice(b datatypes.JSON) []sip_trunk.CodecID {
	if len(b) == 0 {
		return nil
	}
	var out []sip_trunk.CodecID
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
