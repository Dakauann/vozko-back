package workspace_addon

type EntitlementKind string

const (
	EntitlementCallChannels           EntitlementKind = "call_channels"
	EntitlementWhatsAppBusinessPhones EntitlementKind = "whatsapp_business_phones"
	// EntitlementBranches is the number of member SIP extensions (branches) a
	// workspace may create. Plan base (PlanDefinition.MaxBranches) plus active
	// addons, resolved like every other entitlement.
	EntitlementBranches EntitlementKind = "branches"
)

func (k EntitlementKind) IsValid() bool {
	switch k {
	case EntitlementCallChannels, EntitlementWhatsAppBusinessPhones, EntitlementBranches:
		return true
	default:
		return false
	}
}

func AllEntitlementKinds() []EntitlementKind {
	return []EntitlementKind{
		EntitlementCallChannels,
		EntitlementWhatsAppBusinessPhones,
		EntitlementBranches,
	}
}

const BoundResourceWhatsAppBusinessPhone = "whatsapp_business_phone"

type EntitlementResolver interface {
	Resolve(workspaceID string, kind EntitlementKind) (int, error)
}

// BatchEntitlementResolver resolves the effective entitlement for many workspaces
// at once (a handful of batched queries regardless of count), so background
// reconciliation can scan for drift without a per-workspace round trip. It returns
// an entry for every requested workspace (0 when no plan and no addon grant it).
type BatchEntitlementResolver interface {
	ResolveMany(workspaceIDs []string, kind EntitlementKind) (map[string]int, error)
}

type EntitlementChangeHandler interface {
	OnEntitlementReduced(workspaceID string, kind EntitlementKind) error
	OnEntitlementIncreased(workspaceID string, kind EntitlementKind) error
}
