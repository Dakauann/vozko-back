package workspace_addon

type EntitlementKind string

const (
	EntitlementCallChannels           EntitlementKind = "call_channels"
	EntitlementWhatsAppBusinessPhones EntitlementKind = "whatsapp_business_phones"
	// EntitlementBranches is the number of member SIP extensions (branches) a
	// workspace may create. Plan base (PlanDefinition.MaxBranches) plus active
	// addons, resolved like every other entitlement.
	EntitlementBranches EntitlementKind = "branches"
	// EntitlementUnofficialWhatsAppInstances is how many linked-device WhatsApp
	// numbers a workspace may connect.
	//
	// Its base comes from the WORKSPACE CONFIG, not from the plan, and it is the
	// only kind that does. That is deliberate: this channel has no per-plan
	// pricing, every connected number occupies a slot on a host we operate, and
	// its abuse cost is a customer's number being banned — so the included
	// allowance is granted per workspace by a platform administrator rather than
	// implied by whichever plan they happen to be on. Addons top it up exactly
	// like every other kind.
	EntitlementUnofficialWhatsAppInstances EntitlementKind = "unofficial_whatsapp_instances"
)

func (k EntitlementKind) IsValid() bool {
	switch k {
	case EntitlementCallChannels, EntitlementWhatsAppBusinessPhones, EntitlementBranches,
		EntitlementUnofficialWhatsAppInstances:
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
		EntitlementUnofficialWhatsAppInstances,
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
