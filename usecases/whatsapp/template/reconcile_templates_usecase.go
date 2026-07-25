package template_usecase

import (
	"log"
	"strings"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
)

type reconcileTemplatesUseCase struct {
	phoneRepo businessphone.Repository
	syncUC    template.SyncTemplatesUseCase
}

func NewReconcileTemplatesUseCase(phoneRepo businessphone.Repository, syncUC template.SyncTemplatesUseCase) template.ReconcileTemplatesUseCase {
	return &reconcileTemplatesUseCase{phoneRepo: phoneRepo, syncUC: syncUC}
}

func (uc *reconcileTemplatesUseCase) Execute() error {
	phones, err := uc.phoneRepo.ListAll()
	if err != nil {
		return err
	}

	for _, phone := range phones {
		if phone == nil {
			continue
		}
		if !shouldReconcilePhone(phone) {
			continue
		}

		if _, err := uc.syncUC.Execute(template.SyncTemplatesInput{BusinessPhoneID: phone.ID}); err != nil {
			log.Printf("[whatsapp-template-reconcile] failed to sync templates for phone %s (waba=%s): %v", phone.ID, phone.WABAId, err)
		}
	}

	return nil
}

func shouldReconcilePhone(phone *businessphone.WhatsAppBusinessPhoneNumber) bool {
	if phone == nil {
		return false
	}
	if strings.TrimSpace(phone.ID) == "" || strings.TrimSpace(phone.WABAId) == "" {
		return false
	}
	// A phone is reconcilable only if it carries a usable template credential.
	// Meta-hosted phones authenticate with an access token; 360dialog channels
	// authenticate with the channel D360-API-KEY and never have an access token.
	// Requiring an access token here silently excluded every 360dialog number from
	// template sync, so their templates stayed at whatever status create stored
	// (e.g. "pending") forever and never transitioned to approved.
	hasMetaToken := strings.TrimSpace(phone.AccessToken) != ""
	hasDialog360Key := phone.Provider.IsDialog360() && strings.TrimSpace(phone.Dialog360APIKey) != ""
	if !hasMetaToken && !hasDialog360Key {
		return false
	}
	return phone.Status == businessphone.StatusConnected || phone.Status == businessphone.StatusRateLimited
}
