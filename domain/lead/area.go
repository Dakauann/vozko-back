package lead

import (
	"time"

	"vozko/domain/address"
	"vozko/domain/customfield"
)

type Area struct {
	District string
	City     string
	State    string
	CityCode string
}

func (a Area) postal() address.Postal {
	return address.Postal{District: a.District, City: a.City, State: a.State, CityCode: a.CityCode}.Normalize()
}

func areaOf(p address.Postal) Area {
	return Area{District: p.District, City: p.City, State: p.State, CityCode: p.CityCode}
}

func (l *Lead) SetPrimaryArea(area Area) (bool, error) {
	wanted := area.postal()
	primary := l.PrimaryAddress()
	if primary != nil && areaOf(primary.Postal) == areaOf(wanted) {
		return false, nil
	}
	inputs := make([]AddressInput, 0, len(l.Addresses)+1)
	for _, a := range l.Addresses {
		in := AddressInput{ID: a.ID, Label: a.Label, Primary: a.Primary, Postal: a.Postal, KeepFix: true}
		if a.Primary {
			in.Postal.District, in.Postal.City, in.Postal.State, in.Postal.CityCode = wanted.District, wanted.City, wanted.State, wanted.CityCode
		}
		inputs = append(inputs, in)
	}
	if primary == nil {
		inputs = append(inputs, AddressInput{Label: AddressHome, Primary: true, Postal: wanted})
	}
	next := *l
	if err := next.SetAddresses(inputs); err != nil {
		return false, err
	}
	l.Addresses = next.Addresses
	return true, nil
}

type Card struct {
	LeadID         string
	Version        int64
	Name           string
	Number         string
	Blocked        bool
	Owner          string
	OwnerName      string
	OptedOutAt     *time.Time
	OptOutSource   OptOutSource
	Area           *Area
	CustomFields   map[string]any
	RelativesCount int
	ReferredCount  int
}

func CardOf(l *Lead, v Viewer) Card {
	card := Card{
		LeadID: l.ID, Version: l.Version, Name: l.RealName(), Number: l.Number, Blocked: l.Blocked, Owner: l.Owner,
		OptedOutAt: l.OptedOutAt, OptOutSource: l.OptOutSource,
		CustomFields:   customfield.VisibleValues(v.Definitions, v.Fields, l.CustomFields),
		RelativesCount: l.RelativesCount, ReferredCount: l.ReferredCount,
	}
	if primary := l.PrimaryAddress(); primary != nil {
		area := areaOf(primary.area().Postal)
		card.Area = &area
	}
	return card
}
