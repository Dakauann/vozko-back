package customfield

type CreateCustomFieldRequest struct {
	ObjectType string   `json:"objectType" example:"opportunity"`
	Key        string   `json:"key" example:"origem_lead"`
	Label      string   `json:"label" example:"Origem do lead"`
	Type       string   `json:"type" example:"select"`
	Options    []string `json:"options,omitempty"`
	Required   bool     `json:"required,omitempty"`
	Position   int      `json:"position,omitempty"`
}

type UpdateCustomFieldRequest struct {
	Label    *string  `json:"label,omitempty" example:"Origem do lead"`
	Type     *string  `json:"type,omitempty" example:"select"`
	Options  []string `json:"options,omitempty"`
	Required *bool    `json:"required,omitempty"`
	Position *int     `json:"position,omitempty"`
}
