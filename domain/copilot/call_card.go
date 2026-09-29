package copilot

import "vozko/domain/sip_trunk"

type CallIntent struct {
	PhoneNumber string `json:"phoneNumber"`
	TrunkID     string `json:"trunkId,omitempty"`
	TrunkName   string `json:"trunkName,omitempty"`
}

func NewCallCard(plan sip_trunk.CallPlan) *ActionCard {
	intent := CallIntent{PhoneNumber: plan.PhoneNumber}
	if trunk, ok := plan.Chosen(); ok {
		intent.TrunkID = trunk.ID
		intent.TrunkName = trunk.Name
	}
	return &ActionCard{Kind: ActionPlaceCall, Call: &intent}
}
