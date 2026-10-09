package lead_usecase

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
)

var ErrBlockingPhoneUnavailable = errors.New("lead: this business phone cannot apply WhatsApp blocks for the workspace")

type WhatsAppBlocker struct {
	phones PhoneDirectory
	grants businessphone.AccessGrantReader
	meta   MetaBlocking
}

func NewWhatsAppBlocker(phones PhoneDirectory, grants businessphone.AccessGrantReader, meta MetaBlocking) (*WhatsAppBlocker, error) {
	if phones == nil || grants == nil || meta == nil {
		return nil, fmt.Errorf("%w: whatsapp blocker", errCommandsIncomplete)
	}
	return &WhatsAppBlocker{phones: phones, grants: grants, meta: meta}, nil
}

type blockingPhone struct {
	phoneNumberID string
	token         string
	meta          MetaBlocking
}

func (b *WhatsAppBlocker) Phone(workspaceID, businessPhoneID string) (lead.WhatsAppBlock, error) {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	if businessPhoneID == "" {
		return nil, ErrBlockingPhoneUnavailable
	}
	phone, err := b.phones.FindByID(businessPhoneID)
	if errors.Is(err, businessphone.ErrPhoneNumberNotFound) || (err == nil && phone == nil) {
		return nil, fmt.Errorf("%w: %s does not exist", ErrBlockingPhoneUnavailable, businessPhoneID)
	}
	if err != nil {
		return nil, fmt.Errorf("lead: reading business phone %s: %w", businessPhoneID, err)
	}
	allowed, err := businessphone.CanWorkspaceSendFrom(workspaceID, businessPhoneID, phone, b.grants)
	if err != nil {
		return nil, fmt.Errorf("lead: reading the grants of business phone %s: %w", businessPhoneID, err)
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s is not available to workspace %s", ErrBlockingPhoneUnavailable, businessPhoneID, workspaceID)
	}
	if strings.TrimSpace(phone.AccessToken) == "" || strings.TrimSpace(phone.MetaPhoneNumberID) == "" {
		return nil, fmt.Errorf("%w: %s has no access token or Meta phone id", ErrBlockingPhoneUnavailable, businessPhoneID)
	}
	return blockingPhone{phoneNumberID: phone.MetaPhoneNumberID, token: phone.AccessToken, meta: b.meta}, nil
}

func (p blockingPhone) Apply(number string, block bool) error {
	number = strings.TrimSpace(number)
	if number == "" {
		return lead.ErrLeadIdentityRequired
	}
	if block {
		return p.meta.BlockUser(p.phoneNumberID, number, p.token)
	}
	return p.meta.UnblockUser(p.phoneNumberID, number, p.token)
}
