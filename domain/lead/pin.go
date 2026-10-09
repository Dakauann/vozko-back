package lead

import (
	"errors"
	"strings"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/recordevent"
)

const (
	EventLocationPinned   = recordevent.Kind("location_pinned")
	EventLocationAccepted = recordevent.Kind("location_accepted")
)

var (
	ErrLocationInvalid  = errors.New("lead: a pin is an exact point inside Brazil, set by a person or sent by the lead")
	ErrLocationNotFound = errors.New("lead: the message is not a location this lead sent in a conversation you can open")
	ErrAddressNotFound  = errors.New("lead: the address is not one of this lead's")
)

func (l *Lead) PinLocation(addressID string, fix geo.Fix) (bool, error) {
	if err := pinnable(fix, geo.SourceManual); err != nil {
		return false, err
	}
	if !l.IsAggregate() {
		return false, ErrAggregateNotLoaded
	}
	id := strings.TrimSpace(addressID)
	for i, a := range l.Addresses {
		if id != "" && a.ID == id {
			return l.pinAt(i, fix), nil
		}
	}
	return false, ErrAddressNotFound
}

func (l *Lead) AcceptLocation(fix geo.Fix) (bool, error) {
	if err := pinnable(fix, geo.SourceLeadPin); err != nil {
		return false, err
	}
	if !l.IsAggregate() {
		return false, ErrAggregateNotLoaded
	}
	for i, a := range l.Addresses {
		if a.Primary {
			return l.pinAt(i, fix), nil
		}
	}
	pinned := fix
	addresses := make([]Address, 0, len(l.Addresses)+1)
	l.Addresses = append(append(addresses, l.Addresses...), Address{Label: AddressHome, Primary: true, Fix: &pinned, GeoStatus: StatusOfFix(pinned)})
	return true, nil
}

func pinnable(fix geo.Fix, source geo.FixSource) error {
	if fix.Source != source || fix.Precision != geo.PrecisionExact || fix.Validate() != nil || !fix.Point.InBrazil() {
		return ErrLocationInvalid
	}
	return nil
}

func (l *Lead) pinAt(i int, fix geo.Fix) bool {
	next, changed := l.Addresses[i].pinned(fix)
	if !changed {
		return false
	}
	addresses := append([]Address(nil), l.Addresses...)
	addresses[i] = next
	l.Addresses = addresses
	return true
}

func (a Address) pinned(fix geo.Fix) (Address, bool) {
	if a.Fix != nil {
		repeated := fix
		repeated.FixedAt = a.Fix.FixedAt
		if geo.SameFix(a.Fix, &repeated) {
			return a, false
		}
	}
	pinned := fix
	a.Fix, a.GeoStatus = &pinned, StatusOfFix(pinned)
	return a, true
}

func (a Address) withPin(pin *geo.Fix) Address {
	if pin == nil {
		return a
	}
	next, _ := a.pinned(*pin)
	return next
}

func (a Address) positionOnly() bool {
	return a.Postal.Normalize() == (address.Postal{}) && a.Fix != nil && a.Fix.Source.Confirmed()
}

func (a Address) validatePostal() error {
	if a.positionOnly() {
		return nil
	}
	return a.Postal.Validate()
}

func PinEvent(kind recordevent.Kind, actorID string, before, after *Lead) recordevent.Event {
	previous := map[string]*geo.Fix{}
	if before != nil {
		for _, a := range before.Addresses {
			if a.ID != "" {
				previous[a.ID] = a.Fix
			}
		}
	}
	event := recordevent.Event{Actor: actorID, Kind: kind}
	if after == nil {
		return event
	}
	for _, a := range after.Addresses {
		var was *geo.Fix
		if a.ID != "" {
			was = previous[a.ID]
		}
		if geo.SameFix(was, a.Fix) {
			continue
		}
		event.Changes = append(event.Changes, recordevent.Change{Field: FieldAddresses, Before: pinSummary(a.Label, was), After: pinSummary(a.Label, a.Fix)})
	}
	return event
}

func pinSummary(label AddressLabel, fix *geo.Fix) any {
	if fix == nil {
		return nil
	}
	return map[string]any{"label": string(label), "precision": string(fix.Precision), "source": string(fix.Source)}
}
