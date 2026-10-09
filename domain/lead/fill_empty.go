package lead

import (
	"maps"
	"slices"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/shared"
)

type gapFill struct {
	next          *Lead
	line          int
	changed       []string
	conflicts     []string
	issues        []ImportIssue
	newPhones     []ContactPhone
	newAddress    *Address
	filledAddress *Address
}

func newGapFill(l *Lead, line int) *gapFill {
	return &gapFill{next: cloneLead(l), line: line}
}

func (f *gapFill) change(field string) {
	f.changed = append(f.changed, field)
}

func (f *gapFill) conflict(field string) {
	f.conflicts = append(f.conflicts, field)
}

func (f *gapFill) name(source Source, incoming string) {
	if f.next.mergeName(source, incoming) {
		f.change(FieldName)
		return
	}
	normalized, current := NormalizeName(incoming), f.next.RealName()
	if normalized != "" && current != "" && current != normalized && !f.next.isOwnNumber(normalized) {
		f.conflict(FieldName)
	}
}

func (f *gapFill) text(field, stored, incoming string, fill func(*Lead)) {
	switch {
	case incoming == "" || stored == incoming:
	case stored == "":
		fill(f.next)
		f.change(field)
	default:
		f.conflict(field)
	}
}

func (f *gapFill) birthDate(incoming *shared.Date) {
	switch {
	case incoming == nil:
	case f.next.BirthDate == nil:
		date := *incoming
		f.next.BirthDate = &date
		f.change(FieldBirthDate)
	case *f.next.BirthDate != *incoming:
		f.conflict(FieldBirthDate)
	}
}

func (f *gapFill) consent(incoming *Consent) {
	switch {
	case incoming == nil || f.next.WhatsAppOptIn != nil:
	case f.next.OptedOutAt != nil:
		f.conflict(FieldWhatsAppOptIn)
	default:
		consent := *incoming
		f.next.WhatsAppOptIn = &consent
		f.change(FieldWhatsAppOptIn)
	}
}

func (f *gapFill) customFields(incoming map[string]any) map[string]any {
	patch := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(incoming)) {
		value := incoming[key]
		stored := customfield.FormatValue(f.next.CustomFields[key])
		switch {
		case stored == "":
			patch[key] = value
			f.change(CustomFieldName(key))
		case stored != customfield.FormatValue(value):
			f.conflict(CustomFieldName(key))
		}
	}
	return patch
}

func (f *gapFill) phones(incoming []ContactPhone) {
	added := false
	for _, p := range incoming {
		if f.next.HoldsNumber(p.Number) {
			continue
		}
		if len(f.next.Phones) >= MaxContactPhones {
			f.issues = append(f.issues, ImportIssue{Line: f.line, Reason: ReasonPhoneLimit, Field: ImportFieldPhones})
			continue
		}
		f.next.Phones = append(f.next.Phones, p)
		f.newPhones = append(f.newPhones, p)
		added = true
	}
	if added {
		f.change(FieldPhones)
	}
}

func (f *gapFill) address(incoming Address) error {
	postal := incoming.Postal.Normalize()
	if postal == (address.Postal{}) {
		return nil
	}
	primary := f.next.PrimaryAddress()
	if primary == nil {
		if err := postal.Validate(); err != nil {
			return err
		}
		added := incoming
		added.ID, added.Primary, added.Postal = "", true, postal
		if added.Label == "" {
			added.Label = AddressHome
		}
		if added.Fix == nil {
			added.GeoStatus = GeoPending
		}
		f.next.Addresses = append(f.next.Addresses, added)
		f.newAddress = &f.next.Addresses[len(f.next.Addresses)-1]
		f.change(FieldAddresses)
		return nil
	}
	merged, conflict, err := fillPostal(primary.Postal, postal)
	if err != nil {
		return err
	}
	if conflict {
		f.conflict(FieldAddresses)
		return nil
	}
	if merged == primary.Postal {
		return nil
	}
	inputs := make([]AddressInput, 0, len(f.next.Addresses))
	for _, a := range f.next.Addresses {
		in := AddressInput{ID: a.ID, Label: a.Label, Primary: a.Primary, Postal: a.Postal, KeepFix: true}
		if a.Primary {
			in.Postal = merged
		}
		inputs = append(inputs, in)
	}
	if err := f.next.SetAddresses(inputs); err != nil {
		return err
	}
	filled := f.next.PrimaryAddress()
	if incoming.Fix != nil {
		if _, err := filled.ApplyGeocode(*incoming.Fix, filled.Fingerprint()); err != nil {
			return err
		}
	}
	f.filledAddress = filled
	f.change(FieldAddresses)
	return nil
}

func postalParts(p *address.Postal) []*string {
	return []*string{&p.ZipCode, &p.Street, &p.Number, &p.Complement, &p.District, &p.City, &p.State, &p.CityCode}
}

func fillPostal(stored, incoming address.Postal) (address.Postal, bool, error) {
	overlay := stored
	for i, part := range postalParts(&incoming) {
		if *part != "" {
			*postalParts(&overlay)[i] = *part
		}
	}
	if err := overlay.Validate(); err != nil {
		return address.Postal{}, false, err
	}
	merged := stored
	mergedParts := postalParts(&merged)
	for i, part := range postalParts(&incoming) {
		current := mergedParts[i]
		switch {
		case *part == "":
		case *current == "":
			*current = *part
		case shared.FoldForMatch(*current) != shared.FoldForMatch(*part):
			return address.Postal{}, true, nil
		}
	}
	return merged, false, nil
}

func cloneLead(l *Lead) *Lead {
	next := *l
	next.Phones = slices.Clone(l.Phones)
	next.Addresses = slices.Clone(l.Addresses)
	next.Relations = slices.Clone(l.Relations)
	next.CustomFields = maps.Clone(l.CustomFields)
	if next.Phones == nil {
		next.Phones = []ContactPhone{}
	}
	if next.Addresses == nil {
		next.Addresses = []Address{}
	}
	return &next
}

func sortedFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := slices.Clone(fields)
	slices.Sort(out)
	return slices.Compact(out)
}
