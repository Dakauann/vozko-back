package lead

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"time"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

type ProfileSource string

const (
	ProfileFromConversation ProfileSource = "conversation"
	ProfileFromWorkflow     ProfileSource = "workflow"
	ProfileFromForm         ProfileSource = "form"
	profileEventPrefix                    = "profile_from_"
)

var (
	ErrProfileSourceInvalid = errors.New("lead: the source of a profile update is not a known one")
	ErrProfileEmpty         = errors.New("lead: a profile update needs an address, a birth date or a custom field")
	ErrProfileCEPUnknown    = errors.New("lead: the CEP of the profile does not exist")
	ErrProfileCEPUnchecked  = errors.New("lead: the CEP of the profile could not be checked right now")
)

var automationFields = customfield.Viewer{}

func (s ProfileSource) Valid() bool {
	switch s {
	case ProfileFromConversation, ProfileFromWorkflow, ProfileFromForm:
		return true
	}
	return false
}

func (s ProfileSource) EventKind() recordevent.Kind {
	return recordevent.Kind(profileEventPrefix + string(s))
}

type Profile struct {
	Address      address.Postal
	BirthDate    string
	CustomFields map[string]string
}

func (p Profile) Empty() bool {
	return p.Address.Normalize() == (address.Postal{}) && strings.TrimSpace(p.BirthDate) == "" && len(p.customFieldValues()) == 0
}

func (p Profile) customFieldValues() map[string]string {
	values := map[string]string{}
	for key, raw := range p.CustomFields {
		if raw = strings.TrimSpace(raw); raw != "" {
			values[strings.TrimSpace(key)] = raw
		}
	}
	return values
}

func ProfileWritableFields(defs []*customfield.Definition) []*customfield.Definition {
	writable := make([]*customfield.Definition, 0, len(defs))
	for _, def := range defs {
		if customfield.VisibleTo(def, automationFields) {
			writable = append(writable, def)
		}
	}
	return writable
}

type ProfileOutcome struct {
	Changed   []string
	Conflicts []string
}

func (l *Lead) ApplyProfile(p Profile, defs []*customfield.Definition, now time.Time) (ProfileOutcome, error) {
	if !l.IsAggregate() {
		return ProfileOutcome{}, ErrAggregateNotLoaded
	}
	if p.Empty() {
		return ProfileOutcome{}, ErrProfileEmpty
	}
	f := newGapFill(l, 0)
	values, err := profileCustomFields(p.customFieldValues(), defs)
	if err != nil {
		return ProfileOutcome{}, err
	}
	if patch := f.customFields(values); len(patch) > 0 {
		if err := f.next.SetCustomFields(patch, defs, automationFields); err != nil {
			return ProfileOutcome{}, err
		}
	}
	date, err := profileBirthDate(p.BirthDate, now)
	if err != nil {
		return ProfileOutcome{}, err
	}
	f.birthDate(date)
	if err := f.address(Address{Label: AddressHome, Postal: p.Address}); err != nil {
		return ProfileOutcome{}, err
	}
	if err := f.next.ValidateRecord(); err != nil {
		return ProfileOutcome{}, err
	}
	*l = *f.next
	return ProfileOutcome{Changed: sortedFields(f.changed), Conflicts: sortedFields(f.conflicts)}, nil
}

func profileCustomFields(incoming map[string]string, defs []*customfield.Definition) (map[string]any, error) {
	index := make(map[string]*customfield.Definition, len(defs))
	for _, def := range defs {
		if def != nil {
			index[def.Key] = def
		}
	}
	values := make(map[string]any, len(incoming))
	for _, key := range slices.Sorted(maps.Keys(incoming)) {
		def := index[key]
		if def == nil {
			return nil, &customfield.ValueError{Key: key, Err: customfield.ErrUnknownKey}
		}
		if !customfield.VisibleTo(def, automationFields) {
			return nil, &customfield.ValueError{Key: key, Err: customfield.ErrValueForbidden}
		}
		value, err := def.CoerceLocal(incoming[key])
		if err != nil {
			return nil, &customfield.ValueError{Key: key, Err: err}
		}
		values[key] = value
	}
	return values, nil
}

func profileBirthDate(raw string, now time.Time) (*shared.Date, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	date, err := shared.ParseLocalDate(raw)
	if err != nil || !birthDateAllowed(date, now) {
		return nil, ErrLeadBirthDateInvalid
	}
	return &date, nil
}
