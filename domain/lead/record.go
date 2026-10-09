package lead

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/actor"
	"vozko/domain/customfield"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const (
	MaxEmailLength        = 254
	MaxAgeInYears         = 130
	EventCreated          = recordevent.Kind("created")
	EventUpdated          = recordevent.Kind("updated")
	EventRenamed          = recordevent.Kind("renamed")
	EventBlocked          = recordevent.Kind("blocked")
	EventUnblocked        = recordevent.Kind("unblocked")
	EventOwnerChange      = recordevent.Kind("owner_changed")
	EventOptedOut         = recordevent.Kind("opted_out")
	EventMerged           = recordevent.Kind("incoming_merged")
	EventRelationAdded    = recordevent.Kind("relation_added")
	EventRelationRemoved  = recordevent.Kind("relation_removed")
	EventIdentityPromoted = recordevent.Kind("identity_promoted")
)

type Draft struct {
	Number        string
	Name          string
	Nickname      string
	Email         string
	BirthDate     string
	WhatsAppOptIn *bool
	Phones        []ContactPhone
	Addresses     []AddressInput
	CustomFields  map[string]any
}

type Edit struct {
	Number        *string
	Name          *string
	Nickname      *string
	Email         *string
	BirthDate     *string
	WhatsAppOptIn *bool
	Phones        *[]ContactPhone
	Addresses     *[]AddressInput
	CustomFields  map[string]any
}

func New(workspaceID string, d Draft, now time.Time) (*Lead, error) {
	if d.CustomFields != nil {
		return nil, ErrCustomFieldsUnchecked
	}
	l := &Lead{WorkspaceID: strings.TrimSpace(workspaceID), Source: SourceManual, Version: 1,
		Phones: []ContactPhone{}, Addresses: []Address{}, Relations: []Relation{}}
	if raw := strings.TrimSpace(d.Number); raw != "" {
		number, err := shared.ParsePhone(raw)
		if err != nil {
			return nil, ErrLeadInvalid
		}
		l.Number = number
	}
	phones, addresses := d.Phones, d.Addresses
	edit := Edit{Name: &d.Name, Nickname: &d.Nickname, Email: &d.Email, BirthDate: &d.BirthDate, WhatsAppOptIn: d.WhatsAppOptIn,
		Phones: &phones, Addresses: &addresses}
	if err := l.ApplyEdit(edit, now); err != nil {
		return nil, err
	}
	return l, nil
}

func (e Edit) SetsPins() bool {
	return e.Addresses != nil && pinsIn(*e.Addresses)
}

func (d Draft) SetsPins() bool {
	return pinsIn(d.Addresses)
}

func (l *Lead) ApplyEdit(e Edit, now time.Time) error {
	if e.Number != nil {
		return ErrIdentityUnchecked
	}
	if e.CustomFields != nil {
		return ErrCustomFieldsUnchecked
	}
	next := *l
	if e.Name != nil {
		if err := ValidateName(*e.Name); err != nil {
			return err
		}
		if name := NormalizeName(*e.Name); name != next.Name {
			next.Name = name
			next.NameSource = SourceManual
		}
	}
	if e.Nickname != nil {
		next.Nickname = NormalizeName(*e.Nickname)
	}
	if e.Email != nil {
		next.Email = strings.ToLower(strings.TrimSpace(*e.Email))
	}
	if e.BirthDate != nil {
		birth, err := parseBirthDate(*e.BirthDate, now)
		if err != nil {
			return err
		}
		next.BirthDate = birth
	}
	if e.WhatsAppOptIn != nil {
		if *e.WhatsAppOptIn {
			next.grantWhatsAppOptIn(ConsentManual, now)
		} else {
			next.WhatsAppOptIn = nil
		}
	}
	if e.Phones != nil {
		if err := next.SetPhones(*e.Phones); err != nil {
			return err
		}
	}
	if e.Addresses != nil {
		if err := next.SetAddressesAt(*e.Addresses, now); err != nil {
			return err
		}
	}
	if err := next.ValidateRecord(); err != nil {
		return err
	}
	*l = next
	return nil
}

func parseBirthDate(raw string, now time.Time) (*shared.Date, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	date, err := shared.ParseDate(raw)
	if err != nil {
		return nil, ErrLeadBirthDateInvalid
	}
	if !birthDateAllowed(date, now) {
		return nil, ErrLeadBirthDateInvalid
	}
	return &date, nil
}

func birthDateAllowed(date shared.Date, now time.Time) bool {
	return !date.After(now) && date.YearsAt(now) <= MaxAgeInYears
}

func (l *Lead) grantWhatsAppOptIn(source ConsentSource, now time.Time) {
	if l.WhatsAppOptIn == nil {
		l.WhatsAppOptIn = &Consent{GrantedAt: now, Source: source}
	}
	l.OptedOutAt, l.OptOutSource = nil, ""
}

func (l *Lead) Block(by string, at time.Time) bool {
	if l.Blocked {
		return false
	}
	l.Blocked = true
	l.BlockedAt = at
	if by = strings.TrimSpace(by); by != "" {
		l.BlockedBy = &by
	}
	return true
}

func (l *Lead) Unblock() bool {
	if !l.Blocked {
		return false
	}
	l.Blocked = false
	l.BlockedAt = time.Time{}
	l.BlockedBy = nil
	return true
}

func (l *Lead) SetOwner(owner string) (bool, error) {
	owner = strings.TrimSpace(owner)
	if !validOwner(owner) {
		return false, ErrLeadOwnerInvalid
	}
	if owner == l.Owner {
		return false, nil
	}
	l.Owner = owner
	return true, nil
}

func ValidateOwner(owner string) error {
	if !validOwner(owner) {
		return ErrLeadOwnerInvalid
	}
	return nil
}

func validOwner(owner string) bool {
	if owner == "" {
		return true
	}
	id, kind := actor.Split(owner)
	if strings.TrimSpace(id) == "" {
		return false
	}
	switch kind {
	case actor.KindHuman:
		return actor.KindOf(owner) == actor.KindHuman
	case actor.KindAI, actor.KindWorkflow:
		return true
	}
	return false
}

func (l *Lead) OptOut(at time.Time, source OptOutSource) (bool, error) {
	if !source.Valid() {
		return false, ErrLeadOptOutSourceInvalid
	}
	if l.OptedOutAt != nil {
		return false, nil
	}
	l.OptedOutAt, l.OptOutSource = &at, source
	l.WhatsAppOptIn = nil
	return true, nil
}

func ValidateEmail(email string) error {
	if email == "" || utf8.RuneCountInString(email) > MaxEmailLength {
		return ErrLeadEmailInvalid
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" {
		return ErrLeadEmailInvalid
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || !strings.Contains(email[at+1:], ".") {
		return ErrLeadEmailInvalid
	}
	return nil
}

func (l *Lead) RecordFields() map[string]any {
	fields := map[string]any{}
	put := func(field string, value any, filled bool) {
		if filled {
			fields[field] = value
		}
	}
	put(FieldNumber, l.Number, l.Number != "")
	put(FieldName, l.Name, l.Name != "")
	put(FieldNickname, l.Nickname, l.Nickname != "")
	put(FieldEmail, l.Email, l.Email != "")
	put(FieldOwner, l.Owner, l.Owner != "")
	put(FieldWhatsAppOptIn, true, l.WhatsAppOptIn != nil)
	put(FieldOptedOut, true, l.OptedOutAt != nil)
	put(FieldOptOutSource, string(l.OptOutSource), l.OptedOutAt != nil && l.OptOutSource != "")
	put(FieldBlocked, true, l.Blocked)
	if l.BirthDate != nil {
		fields[FieldBirthDate] = l.BirthDate.String()
	}
	put(FieldPhones, phoneFields(l.Phones), len(l.Phones) > 0)
	put(FieldAddresses, addressFields(l.Addresses), len(l.Addresses) > 0)
	for field, value := range l.customFieldRecords() {
		fields[field] = value
	}
	return fields
}

func Changes(kind recordevent.Kind, actorID string, before, after *Lead, defs []*customfield.Definition) recordevent.Event {
	var was map[string]any
	if before != nil {
		was = before.RecordFields()
	}
	event := recordevent.Event{Actor: actorID, Kind: kind, Changes: recordevent.Diff(was, after.RecordFields())}
	return event.Redact(unrecordable(defs))
}

func phoneFields(phones []ContactPhone) []any {
	fields := make([]any, 0, len(phones))
	for _, p := range phones {
		fields = append(fields, map[string]any{"number": p.Number, "label": string(p.Label)})
	}
	return fields
}

func addressFields(addresses []Address) []any {
	fields := make([]any, 0, len(addresses))
	for _, a := range addresses {
		fields = append(fields, map[string]any{
			"label": string(a.Label), "primary": a.Primary,
			"zipCode": a.Postal.ZipCode, "street": a.Postal.Street, "number": a.Postal.Number, "complement": a.Postal.Complement,
			"district": a.Postal.District, "city": a.Postal.City, "state": a.Postal.State,
		})
	}
	return fields
}

func RelationEvent(kind recordevent.Kind, actorID, leadID string, r Relation) recordevent.Event {
	seen := map[string]any{"leadId": r.Other(leadID), "kind": string(r.KindFor(leadID))}
	change := recordevent.Change{Field: FieldRelations}
	if kind == EventRelationRemoved {
		change.Before = seen
	} else {
		change.After = seen
	}
	return recordevent.Event{Actor: actorID, Kind: kind, Changes: []recordevent.Change{change}}
}
