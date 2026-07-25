package whatsapp_repository

import (
	"gorm.io/gorm"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/infra/database/schema"
)

type ownerPhoneReader struct {
	db *gorm.DB
}

func NewOwnerPhoneReader(db *gorm.DB) businessphone.OwnerPhoneReader {
	return &ownerPhoneReader{db: db}
}

func (r *ownerPhoneReader) CountActiveDialog360ByOwner(workspaceID string) (int64, error) {
	var count int64
	err := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Where("owner_workspace_id = ? AND provider = ? AND status NOT IN ?",
			workspaceID,
			string(businessphone.ProviderDialog360),
			[]string{string(businessphone.StatusOnboardingFailed), string(businessphone.StatusSuspended)},
		).
		Count(&count).Error
	return count, err
}

func (r *ownerPhoneReader) CountConnectedDialog360GroupedByOwner() (map[string]int, error) {
	var rows []struct {
		OwnerWorkspaceID string
		Cnt              int
	}
	err := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Select("owner_workspace_id, COUNT(*) AS cnt").
		Where("provider = ? AND owner_workspace_id IS NOT NULL AND status = ?",
			string(businessphone.ProviderDialog360),
			string(businessphone.StatusConnected),
		).
		Group("owner_workspace_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.OwnerWorkspaceID] = row.Cnt
	}
	return out, nil
}

func (r *ownerPhoneReader) ListWorkspaceIDsWithSuspendedDialog360() ([]string, error) {
	var ids []string
	err := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Distinct("owner_workspace_id").
		Where("provider = ? AND owner_workspace_id IS NOT NULL AND status = ?",
			string(businessphone.ProviderDialog360),
			string(businessphone.StatusSuspended),
		).
		Pluck("owner_workspace_id", &ids).Error
	return ids, err
}

func (r *ownerPhoneReader) FindConnectedDialog360ByOwner(workspaceID string) ([]businessphone.OwnerPhone, error) {
	// Newest first: when entitlement drops, the most recently added numbers are
	// the ones suspended (the older numbers the customer relies on are kept).
	return r.findByStatus(workspaceID, businessphone.StatusConnected, "created_at desc")
}

func (r *ownerPhoneReader) FindSuspendedDialog360ByOwner(workspaceID string) ([]businessphone.OwnerPhone, error) {
	// Oldest first: when capacity returns, reactivate the longest-suspended
	// numbers first.
	return r.findByStatus(workspaceID, businessphone.StatusSuspended, "updated_at asc")
}

func (r *ownerPhoneReader) ListDialog360ChannelRefs() ([]businessphone.Dialog360ChannelRef, error) {
	var rows []struct {
		ID                 string
		OwnerWorkspaceID   string
		Dialog360ChannelID string
		Dialog360ClientID  string
		Status             string
	}
	// Same WABA join as findByStatus to carry the client-scoped cancel id. Only
	// phones that actually hold a channel id can be billing at the vendor, so the
	// empty-channel rows are filtered out. Every status is returned: the reconcile
	// needs to know which channels the platform still considers active (CONNECTED) versus
	// already suspended.
	err := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		// COALESCE the nullable uuid owner to '' so an ownerless channel scans cleanly
		// into WorkspaceID (and the vendor reconciler can detect "no owner").
		Select("whatsapp_business_phone_numbers.id AS id, COALESCE(whatsapp_business_phone_numbers.owner_workspace_id::text, '') AS owner_workspace_id, whatsapp_business_phone_numbers.dialog360_channel_id AS dialog360_channel_id, whatsapp_business_phone_numbers.status AS status, w.dialog360_client_id AS dialog360_client_id").
		Joins("LEFT JOIN whatsapp_business_accounts w ON w.meta_waba_id = whatsapp_business_phone_numbers.waba_id").
		Where("whatsapp_business_phone_numbers.provider = ? AND whatsapp_business_phone_numbers.dialog360_channel_id <> ''",
			string(businessphone.ProviderDialog360),
		).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]businessphone.Dialog360ChannelRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, businessphone.Dialog360ChannelRef{
			PhoneID:            row.ID,
			Dialog360ChannelID: row.Dialog360ChannelID,
			Dialog360ClientID:  row.Dialog360ClientID,
			WorkspaceID:        row.OwnerWorkspaceID,
			Active:             businessphone.Status(row.Status) == businessphone.StatusConnected,
		})
	}
	return out, nil
}

func (r *ownerPhoneReader) findByStatus(workspaceID string, status businessphone.Status, order string) ([]businessphone.OwnerPhone, error) {
	var rows []struct {
		ID                 string
		Dialog360ChannelID string
		Dialog360ClientID  string
	}
	// The 360dialog cancel/reactivate endpoints are client scoped, so we join the
	// owning WABA (keyed by the phone's waba_id) to carry its dialog360_client_id.
	// LEFT JOIN so a phone with a missing WABA row still returns (client id empty,
	// handled by the caller) rather than vanishing from the entitlement count.
	err := r.db.Model(&schema.WhatsAppBusinessPhoneNumber{}).
		Select("whatsapp_business_phone_numbers.id AS id, whatsapp_business_phone_numbers.dialog360_channel_id AS dialog360_channel_id, w.dialog360_client_id AS dialog360_client_id").
		Joins("LEFT JOIN whatsapp_business_accounts w ON w.meta_waba_id = whatsapp_business_phone_numbers.waba_id").
		Where("whatsapp_business_phone_numbers.owner_workspace_id = ? AND whatsapp_business_phone_numbers.provider = ? AND whatsapp_business_phone_numbers.status = ?",
			workspaceID,
			string(businessphone.ProviderDialog360),
			string(status),
		).
		Order("whatsapp_business_phone_numbers." + order).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]businessphone.OwnerPhone, 0, len(rows))
	for _, row := range rows {
		out = append(out, businessphone.OwnerPhone{ID: row.ID, Dialog360ChannelID: row.Dialog360ChannelID, Dialog360ClientID: row.Dialog360ClientID})
	}
	return out, nil
}
