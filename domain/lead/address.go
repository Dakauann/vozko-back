package lead

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
)

const MaxAddresses = 5

var (
	ErrAddressLabelInvalid = errors.New("lead: an address label is not a known one")
	ErrAddressLimit        = fmt.Errorf("lead: a lead keeps at most %d addresses", MaxAddresses)
	ErrAddressPrimary      = errors.New("lead: exactly one address is the primary one")
	ErrAddressUnknown      = errors.New("lead: an address is not one of this lead's")
	ErrGeocodeStale        = errors.New("lead: the position was found for an address text that has since changed")
	ErrNoPrimaryAddress    = errors.New("lead: the lead has no primary address to copy")
)

type AddressLabel string

const (
	AddressHome  AddressLabel = "home"
	AddressWork  AddressLabel = "work"
	AddressOther AddressLabel = "other"
)

func (l AddressLabel) Valid() bool {
	switch l {
	case AddressHome, AddressWork, AddressOther:
		return true
	}
	return false
}

type GeoStatus string

const (
	GeoPending       GeoStatus = "pending"
	GeoLocated       GeoStatus = "located"
	GeoApproximate   GeoStatus = "approximate"
	GeoNotFound      GeoStatus = "not_found"
	GeoAmbiguous     GeoStatus = "ambiguous"
	GeoRefused       GeoStatus = "refused"
	GeoUnavailable   GeoStatus = "unavailable"
	GeoQuotaExceeded GeoStatus = "quota_exceeded"
)

func GeoStatuses() []GeoStatus {
	return []GeoStatus{GeoPending, GeoLocated, GeoApproximate, GeoNotFound, GeoAmbiguous, GeoRefused, GeoUnavailable, GeoQuotaExceeded}
}

func QueuedGeoStatuses() []GeoStatus {
	return []GeoStatus{GeoPending, GeoUnavailable, GeoQuotaExceeded}
}

func UnlocatedGeoStatuses() []GeoStatus {
	return []GeoStatus{GeoNotFound, GeoAmbiguous}
}

func SettledGeoStatuses() []GeoStatus {
	return append(UnlocatedGeoStatuses(), GeoQuotaExceeded, GeoRefused)
}

func (s GeoStatus) Queued() bool {
	return slices.Contains(QueuedGeoStatuses(), s)
}

func (s GeoStatus) Valid() bool {
	return slices.Contains(GeoStatuses(), s)
}

type Address struct {
	ID        string         `json:"id,omitempty"`
	Label     AddressLabel   `json:"label"`
	Primary   bool           `json:"primary"`
	Postal    address.Postal `json:"postal"`
	Fix       *geo.Fix       `json:"fix,omitempty"`
	GeoStatus GeoStatus      `json:"geoStatus"`
	CreatedAt time.Time      `json:"createdAt"`
}

type AddressInput struct {
	ID      string
	Label   AddressLabel
	Primary bool
	Postal  address.Postal
	KeepFix bool
	Pin     *geo.Point
}

func pinsIn(inputs []AddressInput) bool {
	for _, in := range inputs {
		if in.Pin != nil {
			return true
		}
	}
	return false
}

func pinOf(in AddressInput, now time.Time) (*geo.Fix, error) {
	if in.Pin == nil {
		return nil, nil
	}
	if now.IsZero() {
		return nil, ErrLocationInvalid
	}
	fix, err := geo.Pinned(*in.Pin, geo.SourceManual, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLocationInvalid, err)
	}
	if err := pinnable(fix, geo.SourceManual); err != nil {
		return nil, err
	}
	return &fix, nil
}

func (a Address) Fingerprint() string {
	return a.Postal.Fingerprint()
}

func (a Address) DistrictKey() string {
	return address.DistrictKey(a.Postal.District)
}

func (a Address) CityKey() string {
	return address.CityKey(a.Postal.City, a.Postal.State)
}

func (a *Address) ApplyGeocode(fix geo.Fix, fingerprint string) (bool, error) {
	if fingerprint != a.Fingerprint() {
		return false, ErrGeocodeStale
	}
	if err := fix.Validate(); err != nil {
		return false, err
	}
	chosen, changed := geo.Choose(a.Fix, []geo.Fix{fix})
	if !changed {
		return false, nil
	}
	a.Fix = &chosen
	a.GeoStatus = StatusOfFix(chosen)
	return true, nil
}

func StatusOfFix(fix geo.Fix) GeoStatus {
	if fix.Precision.PinsAHouse() {
		return GeoLocated
	}
	return GeoApproximate
}

func (l *Lead) SetAddresses(inputs []AddressInput) error {
	return l.SetAddressesAt(inputs, time.Time{})
}

func (l *Lead) SetAddressesAt(inputs []AddressInput, now time.Time) error {
	if len(inputs) > MaxAddresses {
		return ErrAddressLimit
	}
	addresses := make([]Address, 0, len(inputs))
	for i, in := range inputs {
		pin, err := pinOf(in, now)
		if err != nil {
			return itemError(FieldAddresses, i, err)
		}
		next, err := l.addressFrom(in, pin)
		if err != nil {
			return itemError(FieldAddresses, i, err)
		}
		addresses = append(addresses, next)
	}
	if len(addresses) == 1 {
		addresses[0].Primary = true
	}
	candidate := *l
	candidate.Addresses = addresses
	if err := candidate.validateAddresses(); err != nil {
		return err
	}
	l.Addresses = addresses
	return nil
}

func (l *Lead) addressFrom(in AddressInput, pin *geo.Fix) (Address, error) {
	if !in.Label.Valid() {
		return Address{}, ErrAddressLabelInvalid
	}
	next := Address{Label: in.Label, Primary: in.Primary, Postal: in.Postal.Normalize(), GeoStatus: GeoPending}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		next = next.withPin(pin)
		if err := in.Postal.Validate(); err != nil && !next.positionOnly() {
			return Address{}, err
		}
		return next, nil
	}
	if err := in.Postal.Validate(); err != nil && next.Postal != (address.Postal{}) {
		return Address{}, err
	}
	stored, ok := l.storedAddress(id)
	if !ok {
		return Address{}, ErrAddressUnknown
	}
	next.ID, next.CreatedAt = stored.ID, stored.CreatedAt
	if stored.Fingerprint() == next.Fingerprint() || stored.keepsFixThroughEdit(in.KeepFix) {
		next.Fix, next.GeoStatus = stored.Fix, stored.GeoStatus
	}
	next = next.withPin(pin)
	if err := next.validatePostal(); err != nil {
		return Address{}, err
	}
	return next, nil
}

func (a Address) keepsFixThroughEdit(keepAsked bool) bool {
	if a.Fix == nil || !a.Fix.Source.Confirmed() {
		return false
	}
	return keepAsked || a.positionOnly()
}

func (l *Lead) storedAddress(id string) (Address, bool) {
	for _, a := range l.Addresses {
		if a.ID == id {
			return a, true
		}
	}
	return Address{}, false
}

func (l *Lead) validateAddresses() error {
	if len(l.Addresses) > MaxAddresses {
		return ErrAddressLimit
	}
	primaries := 0
	for i, a := range l.Addresses {
		if !a.Label.Valid() {
			return itemError(FieldAddresses, i, ErrAddressLabelInvalid)
		}
		if err := a.validatePostal(); err != nil {
			return itemError(FieldAddresses, i, err)
		}
		if a.Primary {
			primaries++
		}
	}
	if len(l.Addresses) > 0 && primaries != 1 {
		return ErrAddressPrimary
	}
	return nil
}

func (l *Lead) PrimaryAddress() *Address {
	for i := range l.Addresses {
		if l.Addresses[i].Primary {
			return &l.Addresses[i]
		}
	}
	return nil
}

func (l *Lead) AdoptPrimaryAddressOf(other *Lead) error {
	primary := other.PrimaryAddress()
	if primary == nil {
		return ErrNoPrimaryAddress
	}
	copied := *primary
	copied.ID, copied.CreatedAt, copied.Primary = "", time.Time{}, len(l.Addresses) == 0
	if primary.Fix != nil {
		fix := *primary.Fix
		copied.Fix = &fix
	}
	addresses := make([]Address, 0, len(l.Addresses)+1)
	next := *l
	next.Addresses = append(append(addresses, l.Addresses...), copied)
	if err := next.validateAddresses(); err != nil {
		return err
	}
	l.Addresses = next.Addresses
	return nil
}
