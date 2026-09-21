package businessphone_usecase

import (
	"fmt"
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
)

type unassignOwnerUseCase struct {
	repo    businessphone.Repository
	phones  businessphone.OwnerPhoneReader
	partner businessphone.Dialog360PartnerService
}

func NewUnassignOwnerUseCase(
	repo businessphone.Repository,
	phones businessphone.OwnerPhoneReader,
	partner businessphone.Dialog360PartnerService,
) businessphone.UnassignOwnerUseCase {
	return &unassignOwnerUseCase{repo: repo, phones: phones, partner: partner}
}

func (uc *unassignOwnerUseCase) Execute(phoneID string) error {
	if phoneID == "" {
		return businessphone.ErrPhoneNumberNotFound
	}

	phone, err := uc.repo.FindByID(phoneID)
	if err != nil {
		return err
	}

	if phone.OwnerWorkspaceID == "" {
		return nil
	}

	if phone.Provider.IsDialog360() && phone.Status == businessphone.StatusConnected {
		if err := uc.cancelLiveDialog360Channel(phone); err != nil {
			return err
		}
	}

	if err := uc.repo.ClearOwner(phone.ID); err != nil {
		return err
	}

	log.Printf("[unassign-owner] Phone %s (meta_id=%s) returned to the pool (was owned by workspace %s)",
		phone.ID, phone.MetaPhoneNumberID, phone.OwnerWorkspaceID)
	return nil
}

func (uc *unassignOwnerUseCase) cancelLiveDialog360Channel(phone *businessphone.WhatsAppBusinessPhoneNumber) error {
	if uc.partner == nil || uc.phones == nil {
		return fmt.Errorf("unassign: dialog360 channel cancellation is not configured")
	}

	connected, err := uc.phones.FindConnectedDialog360ByOwner(phone.OwnerWorkspaceID)
	if err != nil {
		return err
	}
	var target *businessphone.OwnerPhone
	for i := range connected {
		if connected[i].ID == phone.ID {
			target = &connected[i]
			break
		}
	}
	if target == nil || target.Dialog360ChannelID == "" {
		return nil
	}
	if target.Dialog360ClientID == "" {
		return fmt.Errorf("unassign: cannot cancel dialog360 channel %s: missing client id, cancel it manually before unassigning", target.Dialog360ChannelID)
	}
	if err := uc.partner.CancelChannel(target.Dialog360ClientID, target.Dialog360ChannelID); err != nil {
		return fmt.Errorf("unassign: cancel dialog360 channel %s failed: %w", target.Dialog360ChannelID, err)
	}
	if err := uc.repo.UpdateStatus(phone.ID, businessphone.StatusSuspended); err != nil {
		log.Printf("[unassign-owner] channel %s cancelled but local suspend failed for phone %s: %v", target.Dialog360ChannelID, phone.ID, err)
	}
	log.Printf("[unassign-owner] cancelled dialog360 channel %s before detaching phone %s from workspace %s",
		target.Dialog360ChannelID, phone.ID, phone.OwnerWorkspaceID)
	return nil
}
