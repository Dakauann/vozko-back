package businessphone_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	billing "vozko/domain/billing"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type reconcileVendorChannelsUseCase struct {
	partner businessphone.Dialog360PartnerService
	phones  businessphone.OwnerPhoneReader
	alerter billing.OpsAlerter
}

func NewReconcileVendorChannelsUseCase(
	partner businessphone.Dialog360PartnerService,
	phones businessphone.OwnerPhoneReader,
	alerter billing.OpsAlerter,
) businessphone.VendorChannelReconciler {
	return &reconcileVendorChannelsUseCase{partner: partner, phones: phones, alerter: alerter}
}

func (uc *reconcileVendorChannelsUseCase) Execute() (businessphone.VendorReconcileReport, error) {
	var report businessphone.VendorReconcileReport

	channels, err := uc.partner.ListChannels()
	if err != nil {
		return report, fmt.Errorf("list partner channels: %w", err)
	}
	refs, err := uc.phones.ListDialog360ChannelRefs()
	if err != nil {
		return report, fmt.Errorf("list local channels: %w", err)
	}

	byChannel := make(map[string]businessphone.Dialog360ChannelRef, len(refs))
	for _, r := range refs {
		byChannel[r.Dialog360ChannelID] = r
	}

	for _, ch := range channels {
		if !vendorIsBilling(ch.Status) {
			continue
		}
		report.VendorBilling++

		ref, known := byChannel[ch.ID]
		switch {
		case !known:
			report.Orphans++
			uc.alert("billing: orphan 360dialog channel (no local record)", ch, "")
		case !ref.Active:
			report.Leaks++
			if uc.recancel(ref) {
				report.Recancelled++
				log.Printf("[vendor-reconcile] re-cancelled leaked channel %s (workspace %s)", ch.ID, ref.WorkspaceID)
			} else {
				uc.alert("billing: live 360dialog channel already suspended locally (cancellation lost)", ch, ref.WorkspaceID)
			}
		case strings.TrimSpace(ref.WorkspaceID) == "":
			report.Ownerless++
			if uc.recancel(ref) {
				report.Recancelled++
				log.Printf("[vendor-reconcile] cancelled ownerless live channel %s (no owner)", ch.ID)
			} else {
				uc.alert("billing: live 360dialog channel with no owner workspace (billing for nobody)", ch, "")
			}
		default:
		}
	}
	return report, nil
}

func (uc *reconcileVendorChannelsUseCase) recancel(ref businessphone.Dialog360ChannelRef) bool {
	if uc.partner == nil || ref.Dialog360ClientID == "" {
		return false
	}
	if err := uc.partner.CancelChannel(ref.Dialog360ClientID, ref.Dialog360ChannelID); err != nil {
		log.Printf("[vendor-reconcile] re-cancel of channel %s failed: %v", ref.Dialog360ChannelID, err)
		return false
	}
	return true
}

func (uc *reconcileVendorChannelsUseCase) alert(subject string, ch businessphone.Dialog360Channel, workspaceID string) {
	if uc.alerter == nil {
		return
	}
	detail := fmt.Sprintf("channel_id=%s phone=%s waba=%s status=%s", ch.ID, ch.PhoneNumber, ch.WABAExternalID, ch.Status)
	if workspaceID != "" {
		detail = "workspace=" + workspaceID + " " + detail
	}
	if err := uc.alerter.Alert(context.Background(), subject, detail); err != nil {
		log.Printf("[vendor-reconcile] ops alert failed: %v", err)
	}
}

func vendorIsBilling(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "draft", "sandbox", "unregistered", "pending_deletion", "deleted", "cancelled", "canceled", "terminated", "inactive", "banned":
		return false
	default:
		return true
	}
}
