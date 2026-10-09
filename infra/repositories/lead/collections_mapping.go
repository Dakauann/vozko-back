package lead

import (
	"time"

	"vozko/domain/actor"
	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/lead"
	"vozko/infra/database/schema"
)

func phoneRows(l *lead.Lead, now time.Time) []schema.LeadPhone {
	rows := make([]schema.LeadPhone, 0, len(l.Phones))
	for i, p := range l.Phones {
		createdAt := p.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		rows = append(rows, schema.LeadPhone{
			ID: p.ID, WorkspaceID: l.WorkspaceID, LeadID: l.ID,
			Number: p.Number, Label: string(p.Label), Position: i, CreatedAt: createdAt,
		})
	}
	return rows
}

func phoneOf(row schema.LeadPhone) lead.ContactPhone {
	return lead.ContactPhone{ID: row.ID, Number: row.Number, Label: lead.PhoneLabel(row.Label), CreatedAt: row.CreatedAt}
}

func samePhones(stored []schema.LeadPhone, next []schema.LeadPhone) bool {
	if len(stored) != len(next) {
		return false
	}
	for i := range stored {
		a, b := stored[i], next[i]
		if a.ID != b.ID || a.Number != b.Number || a.Label != b.Label || a.Position != b.Position {
			return false
		}
	}
	return true
}

func addressRow(l *lead.Lead, position int, a lead.Address, now time.Time) schema.LeadAddress {
	p := a.Postal
	row := schema.LeadAddress{
		ID: a.ID, WorkspaceID: l.WorkspaceID, LeadID: l.ID,
		Label: string(a.Label), IsPrimary: a.Primary, Position: position,
		ZipCode: schema.OptionalText(p.ZipCode), Street: schema.OptionalText(p.Street), Number: schema.OptionalText(p.Number),
		Complement: schema.OptionalText(p.Complement), District: schema.OptionalText(p.District),
		DistrictKey: schema.OptionalText(a.DistrictKey()), City: schema.OptionalText(p.City), CityKey: schema.OptionalText(a.CityKey()),
		CityCode: schema.OptionalText(p.CityCode), State: schema.OptionalText(p.State),
		GeoStatus: string(a.GeoStatus), Fingerprint: a.Fingerprint(),
		CreatedAt: a.CreatedAt, UpdatedAt: now,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if a.Fix != nil {
		lat, lng, fixedAt := a.Fix.Point.Lat, a.Fix.Point.Lng, a.Fix.FixedAt
		row.Latitude, row.Longitude, row.GeocodedAt = &lat, &lng, &fixedAt
		row.GeoPrecision = schema.OptionalText(a.Fix.Precision)
		row.GeoSource = schema.OptionalText(a.Fix.Source)
		row.GeoProvider = schema.OptionalText(a.Fix.Provider)
	}
	if a.GeoStatus == lead.GeoPending {
		row.GeoNextAt = &now
	}
	return row
}

func addressOf(row schema.LeadAddress) lead.Address {
	a := lead.Address{
		ID: row.ID, Label: lead.AddressLabel(row.Label), Primary: row.IsPrimary,
		Postal: address.Postal{
			ZipCode: string(row.ZipCode), Street: string(row.Street), Number: string(row.Number), Complement: string(row.Complement),
			District: string(row.District), City: string(row.City), State: string(row.State), CityCode: string(row.CityCode),
		},
		GeoStatus: lead.GeoStatus(row.GeoStatus),
		CreatedAt: row.CreatedAt,
	}
	if row.Latitude != nil && row.Longitude != nil {
		fix := geo.Fix{
			Point:     geo.Point{Lat: *row.Latitude, Lng: *row.Longitude},
			Precision: geo.Precision(row.GeoPrecision),
			Source:    geo.FixSource(row.GeoSource),
			Provider:  string(row.GeoProvider),
		}
		if row.GeocodedAt != nil {
			fix.FixedAt = *row.GeocodedAt
		}
		a.Fix = &fix
	}
	return a
}

func sameAddress(stored, next schema.LeadAddress) bool {
	return stored.Label == next.Label && stored.IsPrimary == next.IsPrimary && stored.Position == next.Position &&
		stored.ZipCode == next.ZipCode && stored.Street == next.Street && stored.Number == next.Number &&
		stored.Complement == next.Complement && stored.District == next.District && stored.City == next.City &&
		stored.CityCode == next.CityCode && stored.State == next.State && stored.Fingerprint == next.Fingerprint &&
		stored.GeoStatus == next.GeoStatus && sameFloat(stored.Latitude, next.Latitude) && sameFloat(stored.Longitude, next.Longitude) &&
		stored.GeoPrecision == next.GeoPrecision && stored.GeoSource == next.GeoSource && stored.GeoProvider == next.GeoProvider
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func relationRow(workspaceID string, r lead.Relation) schema.LeadRelation {
	return schema.LeadRelation{
		ID: r.ID, WorkspaceID: workspaceID, LeadID: r.LeadID, OtherLeadID: r.OtherLeadID,
		Dimension: string(r.Dimension()), Kind: string(r.Kind),
		CreatedBy: schema.OptionalText(humanActor(r.CreatedBy)), CreatedAt: r.CreatedAt,
	}
}

func relationOf(row schema.LeadRelation) lead.Relation {
	return lead.Relation{
		ID: row.ID, LeadID: row.LeadID, OtherLeadID: row.OtherLeadID,
		Kind: lead.RelationKind(row.Kind), CreatedBy: string(row.CreatedBy), CreatedAt: row.CreatedAt,
	}
}

func humanActor(actorID string) string {
	id, kind := actor.Split(actorID)
	if kind != actor.KindHuman {
		return ""
	}
	return id
}
