package businessphone_usecase

import (
	"log"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

type deactivateExcessPhonesUseCase struct {
	entitlements workspace_addon.GetWorkspaceEntitlementsUseCase
	phones       businessphone.OwnerPhoneReader
	phoneRepo    businessphone.Repository
	partner      businessphone.Dialog360PartnerService
	notifier     notification.Notifier
	dashboardURL string
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

// WithNotifier enables the owner email when a number is suspended. dashboardURL is
// the customer app base (CTA target). Returns the use case for chaining at wiring.
func (uc *deactivateExcessPhonesUseCase) WithNotifier(n notification.Notifier, dashboardURL string) *deactivateExcessPhonesUseCase {
	uc.notifier = n
	uc.dashboardURL = dashboardURL
	return uc
}

func (uc *deactivateExcessPhonesUseCase) notifySuspended(workspaceID, phoneID string) {
	if uc.notifier == nil {
		return
	}
	display := ""
	if ph, err := uc.phoneRepo.FindByID(phoneID); err == nil && ph != nil {
		display = ph.DisplayPhoneNumber
	}
	if err := uc.notifier.Notify(notification.Notification{
		WorkspaceID: workspaceID,
		Subject:     "Número de WhatsApp suspenso - " + brand.Active().Name,
		Template:    "whatsapp_number_suspended.html",
		Placeholders: map[string]interface{}{
			"PhoneNumber":  display,
			"Reason":       "Complemento ou plano inativo",
			"DashboardURL": uc.dashboardURL,
		},
		DedupKey: "whatsapp_suspended:" + phoneID,
		DedupTTL: time.Hour,
	}); err != nil {
		log.Printf("[addon-deactivate] suspension email for phone %s failed: %v", phoneID, err)
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
			// Leave the number CONNECTED so the reconcile job retries the cancel.
			// Marking it SUSPENDED here would hide a channel that is still billing.
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
			// Leave the number SUSPENDED so the reconcile job retries the reactivate.
			continue
		}
		if uerr := uc.phoneRepo.UpdateStatus(ph.ID, businessphone.StatusConnected); uerr != nil {
			log.Printf("[addon-reactivate] channel %s reactivated but local update failed for phone %s: %v", ph.Dialog360ChannelID, ph.ID, uerr)
		} else {
			log.Printf("[addon-reactivate] reactivated phone %s (workspace %s)", ph.ID, workspaceID)
		}
	}
	return nil
}

// cancelAtPartner submits the 360dialog cancellation for ph and reports whether the
// channel is now guaranteed to stop billing. A phone with no channel id has no
// billable 360dialog channel, so suspending it locally is safe. A missing client id
// or a failed cancel returns false: the caller then leaves the number CONNECTED for
// the reconcile job to retry, rather than hiding a still-billing channel behind a
// SUSPENDED row.
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

// reactivateAtPartner mirrors cancelAtPartner for the reactivate direction: only on
// a confirmed 360dialog reactivation does the caller flip the row back to CONNECTED.
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
