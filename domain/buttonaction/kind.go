package buttonaction

import (
	"fmt"

	"vozko/domain/workspace"
)

type Kind string

const (
	KindOptOut        Kind = "opt_out"
	KindConsentGrant  Kind = "consent_grant"
	KindHandOff       Kind = "hand_off"
	KindStartWorkflow Kind = "start_workflow"
	KindCallList      Kind = "call_list"
	KindClassify      Kind = "classify"
	KindCreateDeal    Kind = "create_deal"
)

const (
	CapabilityConsent       workspace.CapabilityKey = workspace.CapabilityLeadConsent
	CapabilityAssign        workspace.CapabilityKey = "leads.assign"
	CapabilityManageLists   workspace.CapabilityKey = "call_lists.manage"
	CapabilityEditWorkflows workspace.CapabilityKey = "workflows.edit"
	CapabilityEditDeals     workspace.CapabilityKey = "deals.edit"
)

func Kinds() []Kind {
	return []Kind{KindOptOut, KindConsentGrant, KindHandOff, KindStartWorkflow, KindCallList, KindClassify, KindCreateDeal}
}

func (k Kind) Known() bool {
	for _, known := range Kinds() {
		if k == known {
			return true
		}
	}
	return false
}

func IsExit(k Kind) bool {
	return k == KindOptOut || k == KindConsentGrant
}

type Requirements struct {
	Capabilities []workspace.CapabilityKey
	Permissions  []workspace.PermissionEntry
}

func RequirementsOf(k Kind) (Requirements, error) {
	switch k {
	case KindOptOut:
		return Requirements{}, nil
	case KindConsentGrant:
		return capabilities(CapabilityConsent), nil
	case KindHandOff:
		return capabilities(CapabilityAssign), nil
	case KindCallList:
		return capabilities(CapabilityManageLists), nil
	case KindClassify:
		return Requirements{Permissions: []workspace.PermissionEntry{{Resource: workspace.ResourceLeads, Action: workspace.ActionUpdate}}}, nil
	case KindStartWorkflow:
		return capabilities(CapabilityEditWorkflows), nil
	case KindCreateDeal:
		return capabilities(CapabilityEditDeals), nil
	default:
		return Requirements{}, fmt.Errorf("%w: %q", ErrKindUnknown, k)
	}
}

func capabilities(keys ...workspace.CapabilityKey) Requirements {
	return Requirements{Capabilities: keys}
}

func (r Requirements) Entries() ([]workspace.PermissionEntry, error) {
	entries := append([]workspace.PermissionEntry(nil), r.Permissions...)
	if len(r.Capabilities) == 0 {
		return entries, nil
	}
	required, err := workspace.CapabilitiesRequire(r.Capabilities)
	if err != nil {
		return nil, err
	}
	return append(entries, required...), nil
}
