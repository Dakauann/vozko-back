package workflow

import (
	"fmt"
	"strings"

	"vozko/domain/address"
)

const (
	UpdateLeadName         = "name"
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
	UpdateLeadName, UpdateLeadZipCode, UpdateLeadStreet, UpdateLeadNumber, UpdateLeadComplement,
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

type CustomFieldRefError struct {
	NodeID string
	Key    string
	Err    error
}

func (e *CustomFieldRefError) Error() string {
	return fmt.Sprintf("%s: node %q custom_fields key %q", e.Err.Error(), e.NodeID, e.Key)
}

func (e *CustomFieldRefError) Unwrap() error {
	return e.Err
}

func updateLeadAddress(config map[string]interface{}) address.Postal {
	text := func(key string) string {
		value, _ := config[key].(string)
		return value
	}
	return address.Postal{
		ZipCode: text(UpdateLeadZipCode), Street: text(UpdateLeadStreet), Number: text(UpdateLeadNumber),
		Complement: text(UpdateLeadComplement), District: text(UpdateLeadDistrict), City: text(UpdateLeadCity), State: text(UpdateLeadState),
	}
}

func ValidateUpdateLeadAddress(g *Graph) error {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != NodeTypeActionUpdateLead {
			continue
		}
		postal := updateLeadAddress(n.Config)
		if postal.Normalize() == (address.Postal{}) || postal.Locatable() {
			continue
		}
		return fmt.Errorf("%w: node %q field %q", ErrNodeMissingRequiredField, n.ID, UpdateLeadZipCode)
	}
	return nil
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
