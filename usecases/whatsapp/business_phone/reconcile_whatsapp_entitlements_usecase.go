package businessphone_usecase

import (
	"log"

	businessphone "vozko/domain/whatsapp/business_phone"
	workspace_addon "vozko/domain/workspace/workspace_addon"
)

const reconcileChunkSize = 500

type reconcileWhatsAppEntitlementsUseCase struct {
	phones   businessphone.OwnerPhoneReader
	resolver workspace_addon.BatchEntitlementResolver
	handler  workspace_addon.EntitlementChangeHandler
}

func NewReconcileWhatsAppEntitlementsUseCase(
	phones businessphone.OwnerPhoneReader,
	resolver workspace_addon.BatchEntitlementResolver,
	handler workspace_addon.EntitlementChangeHandler,
) businessphone.EntitlementReconciler {
	return &reconcileWhatsAppEntitlementsUseCase{phones: phones, resolver: resolver, handler: handler}
}

func (uc *reconcileWhatsAppEntitlementsUseCase) Execute() (int, error) {
	connected, err := uc.phones.CountConnectedDialog360GroupedByOwner()
	if err != nil {
		return 0, err
	}
	suspendedList, err := uc.phones.ListWorkspaceIDsWithSuspendedDialog360()
	if err != nil {
		return 0, err
	}

	suspended := make(map[string]struct{}, len(suspendedList))
	candidates := make(map[string]struct{}, len(connected)+len(suspendedList))
	for ws := range connected {
		candidates[ws] = struct{}{}
	}
	for _, ws := range suspendedList {
		suspended[ws] = struct{}{}
		candidates[ws] = struct{}{}
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	ids := make([]string, 0, len(candidates))
	for ws := range candidates {
		ids = append(ids, ws)
	}

	acted := 0
	for start := 0; start < len(ids); start += reconcileChunkSize {
		end := start + reconcileChunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		limits, rerr := uc.resolver.ResolveMany(chunk, workspace_addon.EntitlementWhatsAppBusinessPhones)
		if rerr != nil {
			log.Printf("[whatsapp-reconcile] resolve batch failed (%d workspaces skipped this tick): %v", len(chunk), rerr)
			continue
		}

		for _, ws := range chunk {
			limit, ok := limits[ws]
			if !ok {
				continue
			}
			switch {
			case connected[ws] > limit:
				if err := uc.handler.OnEntitlementReduced(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
					log.Printf("[whatsapp-reconcile] reduce failed for workspace %s: %v", ws, err)
				}
				acted++
			default:
				if _, hasSuspended := suspended[ws]; hasSuspended && limit > connected[ws] {
					if err := uc.handler.OnEntitlementIncreased(ws, workspace_addon.EntitlementWhatsAppBusinessPhones); err != nil {
						log.Printf("[whatsapp-reconcile] increase failed for workspace %s: %v", ws, err)
					}
					acted++
				}
			}
		}
	}
	return acted, nil
}
