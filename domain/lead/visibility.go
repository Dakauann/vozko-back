package lead

import (
	"errors"
	"maps"
	"slices"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/shared"
)

var (
	ErrLeadForbidden             = errors.New("lead: not allowed")
	ErrLeadOwnerOutsideWorkspace = errors.New("lead: the owner is not part of this workspace")
	ErrLeadOwnerOutOfReach       = errors.New("lead: the owner is outside the people you can see")
	ErrAddressesForbidden        = errors.New("lead: changing addresses requires permission to read full addresses")
)

type VersionConflict struct {
	Current *Lead
}

func (e *VersionConflict) Error() string {
	return shared.ErrVersionConflict.Error()
}

func (e *VersionConflict) Unwrap() error {
	return shared.ErrVersionConflict
}

type Viewer struct {
	ReadsLeads     bool
	ReadsAddresses bool
	Fields         customfield.Viewer
	Definitions    []*customfield.Definition
}

func VisibleFields(l *Lead, v Viewer) *Lead {
	if l == nil {
		return nil
	}
	visible := *l
	visible.CustomFields = customfield.VisibleValues(v.Definitions, v.Fields, l.CustomFields)
	if !v.ReadsAddresses && l.Addresses != nil {
		visible.Addresses = make([]Address, 0, len(l.Addresses))
		for _, a := range l.Addresses {
			visible.Addresses = append(visible.Addresses, a.area())
		}
	}
	return &visible
}

func (a Address) area() Address {
	return Address{
		ID: a.ID, Label: a.Label, Primary: a.Primary, GeoStatus: a.GeoStatus, CreatedAt: a.CreatedAt,
		Postal: address.Postal{District: a.Postal.District, City: a.Postal.City, State: a.Postal.State, CityCode: a.Postal.CityCode},
	}
}

func (l *Lead) VisibleTo(v Viewer, sent []string) *Lead {
	if l == nil {
		return nil
	}
	if v.ReadsLeads {
		return VisibleFields(l, v)
	}
	visible := &Lead{ID: l.ID, WorkspaceID: l.WorkspaceID, Version: l.Version, UpdatedAt: l.UpdatedAt}
	for _, field := range sent {
		if key, custom := customFieldKey(field); custom {
			if value, stored := l.CustomFields[key]; stored {
				if visible.CustomFields == nil {
					visible.CustomFields = map[string]any{}
				}
				visible.CustomFields[key] = value
			}
			continue
		}
		switch field {
		case FieldName:
			visible.Name, visible.NameSource = l.Name, l.NameSource
		case FieldNickname:
			visible.Nickname = l.Nickname
		case FieldEmail:
			visible.Email = l.Email
		case FieldBirthDate:
			visible.BirthDate = l.BirthDate
		case FieldOwner:
			visible.Owner = l.Owner
		case FieldWhatsAppOptIn:
			visible.WhatsAppOptIn = l.WhatsAppOptIn
		case FieldOptedOut:
			visible.OptedOutAt, visible.OptOutSource = l.OptedOutAt, l.OptOutSource
		case FieldBlocked:
			visible.Blocked, visible.BlockedAt, visible.BlockedBy = l.Blocked, l.BlockedAt, l.BlockedBy
		case FieldNumber:
			visible.Number = l.Number
		case FieldPhones:
			visible.Phones = l.Phones
		case FieldAddresses:
			visible.Addresses = l.Addresses
		case FieldRelations:
			visible.Relations = l.Relations
			visible.RelativesCount, visible.ReferredCount = l.RelativesCount, l.ReferredCount
		}
	}
	return VisibleFields(visible, v)
}

func (e Edit) Fields() []string {
	var fields []string
	if e.Number != nil {
		fields = append(fields, FieldNumber)
	}
	if e.Name != nil {
		fields = append(fields, FieldName)
	}
	if e.Nickname != nil {
		fields = append(fields, FieldNickname)
	}
	if e.Email != nil {
		fields = append(fields, FieldEmail)
	}
	if e.BirthDate != nil {
		fields = append(fields, FieldBirthDate)
	}
	if e.WhatsAppOptIn != nil {
		fields = append(fields, FieldWhatsAppOptIn)
	}
	if e.Phones != nil {
		fields = append(fields, FieldPhones)
	}
	if e.Addresses != nil {
		fields = append(fields, FieldAddresses)
	}
	for _, key := range slices.Sorted(maps.Keys(e.CustomFields)) {
		fields = append(fields, CustomFieldName(key))
	}
	return fields
}
