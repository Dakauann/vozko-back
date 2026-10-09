package leadimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/sheet"
	"vozko/domain/unofficial_whatsapp"
	"vozko/domain/workspace"
)

const (
	RuleUnknownColumn    = "unknown_column"
	RuleUnknownField     = "unknown_field"
	RuleRepeated         = "repeated"
	RuleTooManyPhones    = "too_many_phones"
	RuleIdentityRequired = "identity_required"
	RuleNeedsPair        = "needs_pair"
	RulePolicy           = "policy"
	RuleScript           = "script"
)

var ErrMappingInvalid = errors.New("lead import: the column mapping cannot be used")

type MappingError struct {
	Column int
	Field  string
	Rule   string
	Err    error
}

func (e *MappingError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%v: %s on column %d (%s): %v", ErrMappingInvalid, e.Rule, e.Column, e.Field, e.Err)
	}
	return fmt.Sprintf("%v: %s on column %d (%s)", ErrMappingInvalid, e.Rule, e.Column, e.Field)
}

func (e *MappingError) Unwrap() error {
	return ErrMappingInvalid
}

type Column struct {
	Index  int    `json:"index"`
	Header string `json:"header,omitempty"`
	Field  string `json:"field"`
}

type Settings struct {
	Columns   []Column                        `json:"columns"`
	Policy    lead.ExistingPolicy             `json:"policy"`
	SeedInbox bool                            `json:"seedInbox,omitempty"`
	Script    *unofficial_whatsapp.SeedScript `json:"script,omitempty"`
}

func (s Settings) mapped() []Column {
	out := make([]Column, 0, len(s.Columns))
	for _, c := range s.Columns {
		if c.Field = strings.TrimSpace(c.Field); c.Field != "" {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Column) int { return a.Index - b.Index })
	return out
}

func (s Settings) Fields() map[string]bool {
	fields := map[string]bool{}
	for _, c := range s.mapped() {
		fields[c.Field] = true
	}
	return fields
}

func (s *Settings) Normalize() {
	for i := range s.Columns {
		s.Columns[i].Field = strings.TrimSpace(s.Columns[i].Field)
	}
	if s.Script != nil {
		s.Script.Normalize()
		if len(s.Script.Bodies) == 0 && s.Script.Attachment == nil && s.Script.MaxMessages == 0 {
			s.Script = nil
		}
	}
}

func (s Settings) Validate(headers []string, defs []*customfield.Definition) error {
	if !s.Policy.Valid() {
		return &MappingError{Column: -1, Field: string(s.Policy), Rule: RulePolicy}
	}
	known := fieldIndex(defs)
	columns, fields, phones := map[int]bool{}, map[string]bool{}, 0
	for _, c := range s.Columns {
		if c.Index < 0 || c.Index >= len(headers) {
			return &MappingError{Column: c.Index, Field: c.Field, Rule: RuleUnknownColumn}
		}
		if columns[c.Index] {
			return &MappingError{Column: c.Index, Field: c.Field, Rule: RuleRepeated}
		}
		columns[c.Index] = true
		field := strings.TrimSpace(c.Field)
		if field == "" {
			continue
		}
		f, ok := known[field]
		if !ok {
			return &MappingError{Column: c.Index, Field: field, Rule: RuleUnknownField}
		}
		if f.Group == GroupPhones {
			if phones++; phones > lead.MaxImportPhones {
				return &MappingError{Column: c.Index, Field: field, Rule: RuleTooManyPhones}
			}
			continue
		}
		if fields[field] {
			return &MappingError{Column: c.Index, Field: field, Rule: RuleRepeated}
		}
		fields[field] = true
	}
	if !fields[FieldNumber] && !fields[FieldName] {
		return &MappingError{Column: -1, Field: FieldNumber, Rule: RuleIdentityRequired}
	}
	for _, pair := range [][2]string{{FieldLatitude, FieldLongitude}, {FieldLongitude, FieldLatitude}, {FieldRelationKind, FieldRelativeNumber}, {FieldConsentPurpose, FieldConsentDate}} {
		if fields[pair[0]] && !fields[pair[1]] {
			return &MappingError{Column: -1, Field: pair[1], Rule: RuleNeedsPair}
		}
	}
	if s.Script != nil {
		if !s.SeedInbox {
			return &MappingError{Column: -1, Field: "seedConversations", Rule: RuleScript}
		}
		if err := s.Script.Validate(); err != nil {
			return &MappingError{Column: -1, Field: "seedConversations", Rule: RuleScript, Err: err}
		}
	}
	return nil
}

func (s Settings) Requirements(defs []*customfield.Definition) []workspace.Action {
	needed := []workspace.Action{workspace.ActionCreate}
	add := func(a workspace.Action) {
		if a != "" && !slices.Contains(needed, a) {
			needed = append(needed, a)
		}
	}
	if s.Policy != lead.PolicySkip {
		add(workspace.ActionUpdate)
	}
	known := fieldIndex(defs)
	for _, c := range s.mapped() {
		add(known[c.Field].Requires)
	}
	order := []workspace.Action{workspace.ActionCreate, workspace.ActionUpdate, workspace.ActionAssign, workspace.ActionReadAddresses, workspace.ActionReadSensitive}
	slices.SortFunc(needed, func(a, b workspace.Action) int { return slices.Index(order, a) - slices.Index(order, b) })
	return needed
}

func (s Settings) Fingerprint() string {
	canonical := struct {
		Columns   []Column                        `json:"c"`
		Policy    lead.ExistingPolicy             `json:"p"`
		SeedInbox bool                            `json:"s"`
		Script    *unofficial_whatsapp.SeedScript `json:"x"`
	}{Policy: s.Policy, SeedInbox: s.SeedInbox, Script: s.Script}
	for _, c := range s.mapped() {
		canonical.Columns = append(canonical.Columns, Column{Index: c.Index, Field: c.Field})
	}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func (s Settings) RowOf(row sheet.Row) lead.ImportRow {
	out := lead.ImportRow{Line: row.Line}
	cell := func(i int) string {
		if i < 0 || i >= len(row.Cells) {
			return ""
		}
		return strings.TrimSpace(row.Cells[i])
	}
	for _, c := range s.mapped() {
		value := cell(c.Index)
		if label, ok := PhoneLabelOf(c.Field); ok {
			out.Phones = append(out.Phones, lead.ImportPhone{Number: value, Label: label})
			continue
		}
		if key, ok := lead.ImportCustomFieldKey(c.Field); ok {
			if out.CustomFields == nil {
				out.CustomFields = map[string]string{}
			}
			out.CustomFields[key] = value
			continue
		}
		assignCell(&out, c.Field, value)
	}
	return out
}

func assignCell(out *lead.ImportRow, field, value string) {
	switch field {
	case FieldNumber:
		out.Number = value
	case FieldName:
		out.Name = value
	case FieldNickname:
		out.Nickname = value
	case FieldEmail:
		out.Email = value
	case FieldBirthDate:
		out.BirthDate = value
	case FieldZipCode:
		out.Postal.ZipCode = value
	case FieldStreet:
		out.Postal.Street = value
	case FieldStreetNumber:
		out.Postal.Number = value
	case FieldComplement:
		out.Postal.Complement = value
	case FieldDistrict:
		out.Postal.District = value
	case FieldCity:
		out.Postal.City = value
	case FieldState:
		out.Postal.State = value
	case FieldLatitude:
		out.Latitude = value
	case FieldLongitude:
		out.Longitude = value
	case FieldOwnerEmail:
		out.OwnerEmail = value
	case FieldConsentDate:
		out.ConsentDate = value
	case FieldConsentPurpose:
		out.ConsentPurpose = value
	case FieldRelativeNumber:
		out.RelativeNumber = value
	case FieldRelationKind:
		out.RelationKind = value
	}
}

func (s Settings) MapsAny(fields ...string) bool {
	mapped := s.Fields()
	for _, f := range fields {
		if mapped[f] {
			return true
		}
	}
	return false
}
