package lead

import (
	"math"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

type ContactPhoneRequest struct {
	ID     string `json:"id,omitempty" example:"2a3b4c5d-6e7f-4a8b-9c0d-1e2f3a4b5c6d"`
	Number string `json:"number" example:"(11) 3333-4444"`
	Label  string `json:"label" example:"landline" enums:"mobile,landline,work,message,other"`
}

type AddressRequest struct {
	ID           string                  `json:"id,omitempty" example:"3c4d5e6f-7a8b-4c9d-8e0f-1a2b3c4d5e6f"`
	Label        string                  `json:"label" example:"home" enums:"home,work,other"`
	Primary      bool                    `json:"primary"`
	ZipCode      string                  `json:"zipCode,omitempty" example:"01310-100"`
	Street       string                  `json:"street,omitempty" example:"Avenida Paulista"`
	Number       string                  `json:"number,omitempty" example:"1000"`
	Complement   string                  `json:"complement,omitempty" example:"apto 12"`
	District     string                  `json:"district,omitempty" example:"Bela Vista"`
	City         string                  `json:"city,omitempty" example:"São Paulo"`
	State        string                  `json:"state,omitempty" example:"SP"`
	CityCode     string                  `json:"cityCode,omitempty" example:"3550308"`
	KeepPosition bool                    `json:"keepPosition,omitempty"`
	Pin          *PinLeadLocationRequest `json:"pin,omitempty"`
}

type ContactPhoneResponse struct {
	ID        string `json:"id"`
	Number    string `json:"number" example:"551133334444"`
	Label     string `json:"label" example:"landline"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type AddressResponse struct {
	ID               string   `json:"id"`
	Label            string   `json:"label" example:"home"`
	Primary          bool     `json:"primary"`
	ZipCode          string   `json:"zipCode,omitempty" example:"01310100"`
	Street           string   `json:"street,omitempty"`
	Number           string   `json:"number,omitempty"`
	Complement       string   `json:"complement,omitempty"`
	District         string   `json:"district,omitempty"`
	City             string   `json:"city,omitempty"`
	State            string   `json:"state,omitempty" example:"SP"`
	CityCode         string   `json:"cityCode,omitempty"`
	GeoStatus        string   `json:"geoStatus" example:"pending" enums:"pending,located,approximate,not_found,ambiguous,refused,unavailable,quota_exceeded"`
	GeoQueued        bool     `json:"geoQueued"`
	Precision        string   `json:"precision,omitempty" example:"postal_code" enums:"exact,address,street,postal_code,district,city"`
	PositionSource   string   `json:"positionSource,omitempty" example:"reference" enums:"manual,lead_pin,import,reference,provider"`
	PositionProvider string   `json:"positionProvider,omitempty" example:"opencage"`
	GeocodedAt       string   `json:"geocodedAt,omitempty" example:"2026-10-08T12:00:00Z"`
	Latitude         *float64 `json:"latitude,omitempty"`
	Longitude        *float64 `json:"longitude,omitempty"`
	CreatedAt        string   `json:"createdAt,omitempty"`
}

type LeadRelativeResponse struct {
	RelationID string `json:"relationId"`
	LeadID     string `json:"leadId"`
	Kind       string `json:"kind" example:"child"`
	Dimension  string `json:"dimension" example:"family" enums:"family,referral"`
	Name       string `json:"name,omitempty" example:"Pedro Souza"`
	Number     string `json:"number,omitempty" example:"5511912345678"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

type RelativesPageResponse struct {
	Items []LeadRelativeResponse `json:"items"`
	Next  string                 `json:"next,omitempty" example:"MjAyNi0xMC0wOFQxMjowMDowMFp8NmYxYzJkM2UtNGI1YS00YzZkLThlN2YtOWEwYjFjMmQzZTRm"`
}

type LeadAreaResponse struct {
	District string `json:"district,omitempty" example:"Bela Vista"`
	City     string `json:"city,omitempty" example:"São Paulo"`
	State    string `json:"state,omitempty" example:"SP"`
	CityCode string `json:"cityCode,omitempty" example:"3550308"`
}

type LeadCardResponse struct {
	LeadID         string            `json:"leadId"`
	Version        int64             `json:"version" example:"4"`
	Name           string            `json:"name,omitempty" example:"Maria Souza"`
	Number         string            `json:"number,omitempty" example:"5511987654321"`
	Blocked        bool              `json:"blocked"`
	Owner          string            `json:"owner,omitempty"`
	OwnerName      string            `json:"ownerName,omitempty" example:"Marina Costa"`
	OptedOutAt     *string           `json:"optedOutAt,omitempty" example:"2026-10-08T12:00:00Z"`
	OptOutSource   string            `json:"optOutSource,omitempty" enums:"operator,lead_request" example:"operator"`
	Area           *LeadAreaResponse `json:"area,omitempty"`
	CustomFields   map[string]any    `json:"customFields,omitempty"`
	RelativesCount int               `json:"relativesCount"`
	ReferredCount  int               `json:"referredCount"`
}

type SetLeadDistrictRequest struct {
	District *string `json:"district" example:"Bela Vista"`
	City     *string `json:"city" example:"São Paulo"`
	State    *string `json:"state" example:"SP"`
	CityCode string  `json:"cityCode,omitempty" example:"3550308"`
}

type DuplicateCandidateResponse struct {
	LeadID  string   `json:"leadId"`
	Reasons []string `json:"reasons" example:"shared_phone"`
	Name    string   `json:"name,omitempty" example:"João Souza"`
	Number  string   `json:"number,omitempty" example:"5511912345678"`
}

type CreateLeadResponse struct {
	LeadRecordResponse
	Duplicates []DuplicateCandidateResponse `json:"duplicates"`
}

type AddRelativeRequest struct {
	Kind               string             `json:"kind" example:"child"`
	Relative           *CreateLeadRequest `json:"relative"`
	CopyPrimaryAddress bool               `json:"copyPrimaryAddress,omitempty"`
}

type LinkRelationRequest struct {
	OtherLeadID string `json:"otherLeadId" example:"1d2c3b4a-5f6e-4d7c-8b9a-0f1e2d3c4b5a"`
	Kind        string `json:"kind" example:"referred_by"`
}

type LeadRelationResponse struct {
	ID         string `json:"id"`
	LeadID     string `json:"leadId"`
	RelativeID string `json:"relativeId"`
	Kind       string `json:"kind" example:"parent"`
	Dimension  string `json:"dimension" example:"family" enums:"family,referral"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

type AddRelativeResponse struct {
	Lead       LeadRecordResponse           `json:"lead"`
	Relative   LeadRecordResponse           `json:"relative"`
	Relation   LeadRelationResponse         `json:"relation"`
	Duplicates []DuplicateCandidateResponse `json:"duplicates"`
}

type LinkRelationResponse struct {
	Lead     LeadRecordResponse   `json:"lead"`
	Relation LeadRelationResponse `json:"relation"`
}

func phoneInputs(requests []ContactPhoneRequest) []leaddomain.ContactPhone {
	phones := make([]leaddomain.ContactPhone, 0, len(requests))
	for _, p := range requests {
		phones = append(phones, leaddomain.ContactPhone{ID: p.ID, Number: p.Number, Label: leaddomain.PhoneLabel(p.Label)})
	}
	return phones
}

func addressInputs(requests []AddressRequest) []leaddomain.AddressInput {
	addresses := make([]leaddomain.AddressInput, 0, len(requests))
	for _, a := range requests {
		addresses = append(addresses, leaddomain.AddressInput{
			ID: a.ID, Label: leaddomain.AddressLabel(a.Label), Primary: a.Primary, KeepFix: a.KeepPosition, Pin: a.Pin.point(),
			Postal: address.Postal{
				ZipCode: a.ZipCode, Street: a.Street, Number: a.Number, Complement: a.Complement,
				District: a.District, City: a.City, State: a.State, CityCode: a.CityCode,
			},
		})
	}
	return addresses
}

func phoneResponses(phones []leaddomain.ContactPhone) []ContactPhoneResponse {
	if phones == nil {
		return nil
	}
	out := make([]ContactPhoneResponse, 0, len(phones))
	for _, p := range phones {
		out = append(out, ContactPhoneResponse{ID: p.ID, Number: p.Number, Label: string(p.Label), CreatedAt: fmtNonZero(p.CreatedAt)})
	}
	return out
}

func addressResponses(addresses []leaddomain.Address) []AddressResponse {
	if addresses == nil {
		return nil
	}
	out := make([]AddressResponse, 0, len(addresses))
	for _, a := range addresses {
		p := a.Postal
		item := AddressResponse{
			ID: a.ID, Label: string(a.Label), Primary: a.Primary,
			ZipCode: p.ZipCode, Street: p.Street, Number: p.Number, Complement: p.Complement,
			District: p.District, City: p.City, State: p.State, CityCode: p.CityCode,
			GeoStatus: string(a.GeoStatus), GeoQueued: a.GeoStatus.Queued(), CreatedAt: fmtNonZero(a.CreatedAt),
		}
		if a.Fix != nil {
			lat, lng := a.Fix.Point.Lat, a.Fix.Point.Lng
			item.Latitude, item.Longitude = &lat, &lng
			item.Precision, item.PositionSource = string(a.Fix.Precision), string(a.Fix.Source)
			item.PositionProvider, item.GeocodedAt = a.Fix.Provider, fmtNonZero(a.Fix.FixedAt)
		}
		out = append(out, item)
	}
	return out
}

func relationResponse(r leaddomain.Relation, seenFrom string) LeadRelationResponse {
	return LeadRelationResponse{
		ID: r.ID, LeadID: seenFrom, RelativeID: r.Other(seenFrom),
		Kind: string(r.KindFor(seenFrom)), Dimension: string(r.Dimension()), CreatedAt: fmtNonZero(r.CreatedAt),
	}
}

func relativesPageResponse(page leaddomain.RelativesPage, seenFrom string) RelativesPageResponse {
	out := RelativesPageResponse{Items: make([]LeadRelativeResponse, 0, len(page.Relatives)), Next: page.Next}
	for _, r := range page.Relatives {
		kind := r.KindFrom(seenFrom)
		item := LeadRelativeResponse{
			RelationID: r.Relation.ID, Kind: string(kind), Dimension: string(kind.Dimension()), CreatedAt: fmtNonZero(r.Relation.CreatedAt),
		}
		if r.Lead != nil {
			item.LeadID, item.Name, item.Number = r.Lead.ID, r.Lead.RealName(), r.Lead.Number
		}
		out.Items = append(out.Items, item)
	}
	return out
}

func leadCardResponse(c leaddomain.Card) LeadCardResponse {
	out := LeadCardResponse{
		LeadID: c.LeadID, Version: c.Version, Name: c.Name, Number: c.Number, Blocked: c.Blocked, Owner: c.Owner, OwnerName: c.OwnerName,
		OptedOutAt: fmtTimePtr(c.OptedOutAt), OptOutSource: string(c.OptOutSource),
		CustomFields: c.CustomFields, RelativesCount: c.RelativesCount, ReferredCount: c.ReferredCount,
	}
	if c.Area != nil {
		out.Area = &LeadAreaResponse{District: c.Area.District, City: c.Area.City, State: c.Area.State, CityCode: c.Area.CityCode}
	}
	return out
}

func duplicateResponses(warnings []lead_usecase.DuplicateWarning) []DuplicateCandidateResponse {
	out := make([]DuplicateCandidateResponse, 0, len(warnings))
	for _, w := range warnings {
		item := DuplicateCandidateResponse{LeadID: w.LeadID, Reasons: make([]string, 0, len(w.Reasons))}
		for _, reason := range w.Reasons {
			item.Reasons = append(item.Reasons, string(reason))
		}
		if w.Lead != nil {
			item.Name, item.Number = w.Lead.RealName(), w.Lead.Number
		}
		out = append(out, item)
	}
	return out
}

func toRelativeResponse(result lead_usecase.RelativeResult, now time.Time) AddRelativeResponse {
	return AddRelativeResponse{
		Lead:       toLeadRecord(result.Lead, now),
		Relative:   toLeadRecord(result.Relative, now),
		Relation:   relationResponse(result.Relation, result.Lead.ID),
		Duplicates: duplicateResponses(result.Duplicates),
	}
}

func (r *PinLeadLocationRequest) point() *geo.Point {
	if r == nil {
		return nil
	}
	p := geo.Point{Lat: math.NaN(), Lng: math.NaN()}
	if r.Latitude != nil {
		p.Lat = *r.Latitude
	}
	if r.Longitude != nil {
		p.Lng = *r.Longitude
	}
	return &p
}
