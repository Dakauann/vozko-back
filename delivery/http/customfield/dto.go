package customfield

type CreateCustomFieldRequest struct {
	ObjectType  string            `json:"objectType" example:"lead" enums:"opportunity,lead"`
	Key         string            `json:"key" example:"classificacao"`
	Label       string            `json:"label" example:"Classificação"`
	Type        string            `json:"type" example:"select" enums:"text,number,date,boolean,select,multiselect"`
	Options     []string          `json:"options,omitempty"`
	OptionTones map[string]string `json:"optionTones,omitempty" example:"Positivo:chart-1"`
	Required    bool              `json:"required,omitempty"`
	Sensitive   *bool             `json:"sensitive,omitempty" example:"true"`
	LegalBasis  string            `json:"legalBasis,omitempty" example:"Consentimento do titular para comunicação política"`
	Role        string            `json:"role,omitempty" example:"classification" enums:"classification"`
	Position    int               `json:"position,omitempty"`
}

type UpdateCustomFieldRequest struct {
	Label       *string           `json:"label,omitempty" example:"Classificação"`
	Type        *string           `json:"type,omitempty" example:"select" enums:"text,number,date,boolean,select,multiselect"`
	Options     []string          `json:"options,omitempty"`
	OptionTones map[string]string `json:"optionTones,omitempty" example:"Positivo:chart-1"`
	Required    *bool             `json:"required,omitempty"`
	Sensitive   *bool             `json:"sensitive,omitempty" example:"true"`
	LegalBasis  *string           `json:"legalBasis,omitempty" example:"Consentimento do titular para comunicação política"`
	Role        *string           `json:"role,omitempty" example:"classification" enums:"classification"`
	Position    *int              `json:"position,omitempty"`
}
