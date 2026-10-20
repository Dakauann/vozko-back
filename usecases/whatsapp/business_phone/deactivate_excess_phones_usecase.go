package businessphone_usecase

import (
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type deactivateExcessPhonesUseCase struct {
	entitlements workspace_addon.GetWorkspaceEntitlementsUseCase
	phones       businessphone.OwnerPhoneReader
	phoneRepo    businessphone.Repository
	partner      businessphone.Dialog360PartnerService
	notices      businessphone.PhoneNotifier
}

var _ workspace_addon.EntitlementChangeHandler = (*deactivateExcessPhonesUseCase)(nil)

func NewDeactivateExcessPhonesUseCase(
	entitlements workspace_addon.GetWorkspaceEntitlementsUseCase,
	phones businessphone.OwnerPhoneReader,
	phoneRepo businessphone.Repository,
	partner businessphone.Dialog360PartnerService,
) *deactivateExcessPhonesUseCase {
	return &deactivateExcessPhonesUseCase{
		entitlements: entitlements,
		phones:       phones,
		phoneRepo:    phoneRepo,
		partner:      partner,
	}
}

func (uc *deactivateExcessPhonesUseCase) WithNotices(notices businessphone.PhoneNotifier) *deactivateExcessPhonesUseCase {
	uc.notices = notices
	return uc
}

func (uc *deactivateExcessPhonesUseCase) notifySuspended(workspaceID, phoneID string) {
	if uc.notices == nil {
		return
	}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: phoneID, OwnerWorkspaceID: workspaceID}
	if found, err := uc.phoneRepo.FindByID(phoneID); err == nil && found != nil {
		phone.DisplayPhoneNumber = found.DisplayPhoneNumber
	}
	if err := uc.notices.NotifyPhone(phone, businessphone.SuspendedNotice()); err != nil {
		log.Printf("[addon-deactivate] suspension notification for phone %s failed: %v", phoneID, err)
	}
}

func (uc *deactivateExcessPhonesUseCase) clearSuspended(workspaceID, phoneID string) {
	if uc.notices == nil {
		return
	}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: phoneID, OwnerWorkspaceID: workspaceID}
	if err := uc.notices.ClearPhone(phone, businessphone.SuspendedNoticeState); err != nil {
		log.Printf("[addon-reactivate] suspension alert of phone %s could not be closed: %v", phoneID, err)
	}
}

func (uc *deactivateExcessPhonesUseCase) OnEntitlementReduced(workspaceID string, kind workspace_addon.EntitlementKind) error {
	if kind != workspace_addon.EntitlementWhatsAppBusinessPhones {
		return nil
	}

	limit := 0
	ents, err := uc.entitlements.Execute(workspaceID)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Kind == kind {
			limit = e.Total
			break
		}
	}

	connected, err := uc.phones.FindConnectedDialog360ByOwner(workspaceID)
	if err != nil {
		return err
	}
	excess := len(connected) - limit
	if excess <= 0 {
		return nil
	}

	for i := 0; i < excess && i < len(connected); i++ {
		ph := connected[i]
		if !uc.cancelAtPartner(ph) {
			continue
		}
		if uerr := uc.phoneRepo.UpdateStatus(ph.ID, businessphone.StatusSuspended); uerr != nil {
			log.Printf("[addon-deactivate] channel %s cancelled but local suspend failed for phone %s: %v", ph.Dialog360ChannelID, ph.ID, uerr)
		} else {
			log.Printf("[addon-deactivate] suspended phone %s (workspace %s, over entitlement)", ph.ID, workspaceID)
			uc.notifySuspended(workspaceID, ph.ID)
		}
	}
	return nil
}

func (uc *deactivateExcessPhonesUseCase) OnEntitlementIncreased(workspaceID string, kind workspace_addon.EntitlementKind) error {
	if kind != workspace_addon.EntitlementWhatsAppBusinessPhones {
		return nil
	}

	limit := 0
	ents, err := uc.entitlements.Execute(workspaceID)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Kind == kind {
			limit = e.Total
			break
		}
	}

	active, err := uc.phones.CountActiveDialog360ByOwner(workspaceID)
	if err != nil {
		return err
	}
	room := limit - int(active)
	if room <= 0 {
		return nil
	}

	suspended, err := uc.phones.FindSuspendedDialog360ByOwner(workspaceID)
	if err != nil {
		return err
	}
	for i := 0; i < room && i < len(suspended); i++ {
		ph := suspended[i]
		if !uc.reactivateAtPartner(ph) {
			continue
		}
		if uerr := uc.phoneRepo.UpdateStatus(ph.ID, businessphone.StatusConnected); uerr != nil {
			log.Printf("[addon-reactivate] channel %s reactivated but local update failed for phone %s: %v", ph.Dialog360ChannelID, ph.ID, uerr)
		} else {
			log.Printf("[addon-reactivate] reactivated phone %s (workspace %s)", ph.ID, workspaceID)
			uc.clearSuspended(workspaceID, ph.ID)
		}
	}
	return nil
}

func (uc *deactivateExcessPhonesUseCase) cancelAtPartner(ph businessphone.OwnerPhone) bool {
	if ph.Dialog360ChannelID == "" || uc.partner == nil {
		return true
	}
	if ph.Dialog360ClientID == "" {
		log.Printf("[addon-deactivate] cannot cancel channel %s: missing 360dialog client id (manual cancellation needed to stop billing); left connected for retry", ph.Dialog360ChannelID)
		return false
	}
	if cerr := uc.partner.CancelChannel(ph.Dialog360ClientID, ph.Dialog360ChannelID); cerr != nil {
		log.Printf("[addon-deactivate] cancel channel %s failed: %v; left connected for retry", ph.Dialog360ChannelID, cerr)
		return false
	}
	return true
}

func (uc *deactivateExcessPhonesUseCase) reactivateAtPartner(ph businessphone.OwnerPhone) bool {
	if ph.Dialog360ChannelID == "" || uc.partner == nil {
		return true
	}
	if ph.Dialog360ClientID == "" {
		log.Printf("[addon-reactivate] cannot reactivate channel %s: missing 360dialog client id (manual reactivation needed); left suspended for retry", ph.Dialog360ChannelID)
		return false
	}
	if rerr := uc.partner.ReactivateChannel(ph.Dialog360ClientID, ph.Dialog360ChannelID); rerr != nil {
		log.Printf("[addon-reactivate] reactivate channel %s failed: %v; left suspended for retry", ph.Dialog360ChannelID, rerr)
		return false
	}
	return true
}
