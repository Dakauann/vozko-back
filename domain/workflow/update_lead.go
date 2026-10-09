package workflow

import (
	"fmt"
	"strings"
)

const (
	UpdateLeadZipCode      = "zip_code"
	UpdateLeadStreet       = "street"
	UpdateLeadNumber       = "number"
	UpdateLeadComplement   = "complement"
	UpdateLeadDistrict     = "district"
	UpdateLeadCity         = "city"
	UpdateLeadState        = "state"
	UpdateLeadBirthDate    = "birth_date"
	UpdateLeadCustomFields = "custom_fields"
)

var UpdateLeadTextFields = []string{
	UpdateLeadZipCode, UpdateLeadStreet, UpdateLeadNumber, UpdateLeadComplement,
	UpdateLeadDistrict, UpdateLeadCity, UpdateLeadState, UpdateLeadBirthDate,
}

func UpdateLeadCustomFieldsOf(config map[string]interface{}) map[string]string {
	raw, _ := config[UpdateLeadCustomFields].(map[string]interface{})
	fields := make(map[string]string, len(raw))
	for key, value := range raw {
		key = strings.TrimSpace(key)
		text := strings.TrimSpace(fmt.Sprint(value))
		if key == "" || value == nil || text == "" {
			continue
		}
		fields[key] = text
	}
	return fields
}

func updateLeadWritesSomething(config map[string]interface{}) bool {
	for _, key := range UpdateLeadTextFields {
		if value, _ := config[key].(string); strings.TrimSpace(value) != "" {
			return true
		}
	}
	return len(UpdateLeadCustomFieldsOf(config)) > 0
}

func ValidateUpdateLeadConfig(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionUpdateLead || updateLeadWritesSomething(n.Config) {
			continue
		}
		return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, UpdateLeadCustomFields)
	}
	return nil
}
