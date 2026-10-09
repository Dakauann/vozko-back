package siptrunk

type DialTargetNumberResponse struct {
	Number   string `json:"number" example:"5584994409684"`
	Identity bool   `json:"identity" example:"true"`
	Label    string `json:"label,omitempty" enums:"mobile,landline,work,message,other" example:"landline"`
	PhoneID  string `json:"phoneId,omitempty" example:"0c2d4e6f-8a1b-4c3d-9e5f-7a6b5c4d3e2f"`
	Refusal  string `json:"refusal,omitempty" enums:"blocked,opted_out,invalid_number" example:"opted_out"`
}

type DialTargetTrunkResponse struct {
	ID   string `json:"id" example:"7b0c9a52-1d2e-4f3a-9b8c-0d1e2f3a4b5c"`
	Name string `json:"name" example:"Operadora principal"`
}

type DialTargetsResponse struct {
	LeadID       string                     `json:"leadId" example:"6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01"`
	Refusal      string                     `json:"refusal,omitempty" enums:"blocked,no_number,opted_out,invalid_number" example:"blocked"`
	Callable     string                     `json:"callable,omitempty" example:"5584994409684"`
	Numbers      []DialTargetNumberResponse `json:"numbers"`
	Trunks       []DialTargetTrunkResponse  `json:"trunks"`
	TrunkRefusal string                     `json:"trunkRefusal,omitempty" enums:"unauthorized,no_dialable_trunk" example:"no_dialable_trunk"`
}
