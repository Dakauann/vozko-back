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

// NewReconcileVendorChannelsUseCase builds the daily the platform-vs-360dialog reconciliation. It is the
// financial backstop for a silently lost cancellation: where the internal EntitlementReconciler keeps
// the platform's own view consistent with entitlement, this keeps the platform's view consistent with the vendor's
// actual channel listing, so 360dialog never bills a channel the platform is not collecting for.
func NewReconcileVendorChannelsUseCase(
	partner businessphone.Dialog360PartnerService,
	phones businessphone.OwnerPhoneReader,
	alerter billing.OpsAlerter,
) businessphone.VendorChannelReconciler {
	return &reconcileVendorChannelsUseCase{partner: partner, phones: phones, alerter: alerter}
}

// Execute lists the partner's actual channels and, for every channel the vendor still bills (status
// live), checks the platform's view:
//
//   - no the platform record -> ORPHAN: the vendor bills a channel the platform cannot even identify (there is no
//     client id to cancel it). Raise an ops alert for manual action.
//   - suspended locally -> LEAK: the platform already cancelled (or lapsed) this channel, but the cancellation
//     never reached the vendor. Re-issue it; alert if it still fails.
//   - active locally, no owner -> OWNERLESS: billing 360dialog but funded by no plan/customer and
//     un-reactivatable (reactivation is owner-keyed). Cancel it, nobody can ever pay for it.
//   - active locally, owned -> consistent. The internal EntitlementReconciler owns the over-cap case where
//     an active owned channel should be suspended; this pass does not duplicate that entitlement check.
//
// It is read-mostly and idempotent: 360dialog's cancellation_request is reversible and re-submitting it
// for an already-suspended channel is a no-op, so a transient divergence self-heals on the next pass.
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
			continue // pending_deletion / draft / etc: the vendor will not bill it next month
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
			// Live at Vozko but owned by no workspace: 360dialog bills it, yet no plan or
			// customer funds it and it can never be reactivated (reactivation is owner-keyed,
			// see OnEntitlementIncreased). Cancel it, the vendor cancellation_request is
			// reversible if the number is ever re-assigned. This is the ownerless-billing
			// backstop; without it an unassigned live channel bills forever.
			report.Ownerless++
			if uc.recancel(ref) {
				report.Recancelled++
				log.Printf("[vendor-reconcile] cancelled ownerless live channel %s (no owner)", ch.ID)
			} else {
				uc.alert("billing: live 360dialog channel with no owner workspace (billing for nobody)", ch, "")
			}
		default:
			// consistent: the channel is active locally, owned, and the vendor bills it.
		}
	}
	return report, nil
}

// recancel re-submits the client-scoped cancellation for a leaked channel and reports whether it is
// now guaranteed to stop billing. Without the client id the channel cannot be cancelled here, so the
// caller alerts instead.
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

// vendorIsBilling reports whether a partner channel status means 360dialog will bill the channel for
// the next calendar month. Only a live channel bills; a channel with a registered cancellation shows as
// pending_deletion and stops at month end. The exact partner status vocabulary is a vendor-verification
// item (UNIFIED_BILLING_IMPLEMENTATION_PLAN.md section 17); any status not known to be non-billing is
// treated as billing so a real leak is never silently skipped (re-cancelling a channel already
// suspended is a safe no-op).
func vendorIsBilling(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "draft", "sandbox", "unregistered", "pending_deletion", "deleted", "cancelled", "canceled", "terminated", "inactive", "banned":
		return false
	default:
		return true
	}
}
