package lead

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/address"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	"vozko/domain/shared"
)

const (
	MaxImportRows           = 200000
	MaxImportPhones         = 4
	MaxConsentPurposeLength = 200
	importCustomFieldPrefix = "custom_field:"
)

var (
	ErrImportEmpty = errors.New("lead: import has no rows")

	ErrImportTooManyRows = errors.New("lead: too many rows for a single import")
)

const (
	ImportFieldNumber         = "number"
	ImportFieldName           = "name"
	ImportFieldNickname       = "nickname"
	ImportFieldEmail          = "email"
	ImportFieldBirthDate      = "birth_date"
	ImportFieldPhones         = "phones"
	ImportFieldAddress        = "address"
	ImportFieldCoordinates    = "coordinates"
	ImportFieldOwner          = "owner_email"
	ImportFieldConsentDate    = "consent_date"
	ImportFieldConsentPurpose = "consent_purpose"
	ImportFieldRelative       = "relative_number"
	ImportFieldRelationKind   = "relation_kind"
)

func ImportCustomField(key string) string {
	return importCustomFieldPrefix + key
}

func ImportCustomFieldKey(field string) (string, bool) {
	key, ok := strings.CutPrefix(field, importCustomFieldPrefix)
	return key, ok && key != ""
}

type ExistingPolicy string

const (
	PolicyFillEmpty ExistingPolicy = "fill_empty"

	PolicySkip ExistingPolicy = "skip"
)

func (p ExistingPolicy) Valid() bool {
	return p == PolicyFillEmpty || p == PolicySkip
}

func ParseExistingPolicy(value string) (ExistingPolicy, bool) {
	switch ExistingPolicy(strings.TrimSpace(value)) {
	case "":
		return PolicyFillEmpty, true
	case PolicyFillEmpty:
		return PolicyFillEmpty, true
	case PolicySkip:
		return PolicySkip, true
	default:
		return "", false
	}
}

type RejectReason string

const (
	ReasonInvalid                  RejectReason = "invalid"
	ReasonDuplicate                RejectReason = "duplicate"
	ReasonIdentityRequired         RejectReason = "identity_required"
	ReasonNameTooLong              RejectReason = "name_too_long"
	ReasonNicknameTooLong          RejectReason = "nickname_too_long"
	ReasonEmailInvalid             RejectReason = "email_invalid"
	ReasonBirthDateInvalid         RejectReason = "birth_date_invalid"
	ReasonPhoneInvalid             RejectReason = "phone_invalid"
	ReasonPhoneLimit               RejectReason = "phone_limit"
	ReasonAddressInvalid           RejectReason = "address_invalid"
	ReasonCoordinatesInvalid       RejectReason = "coordinates_invalid"
	ReasonCoordinatesOutsideBrazil RejectReason = "coordinates_outside_brazil"
	ReasonOwnerNotFound            RejectReason = "owner_not_found"
	ReasonOwnerOutOfReach          RejectReason = "owner_out_of_reach"
	ReasonConsentDateInvalid       RejectReason = "consent_date_invalid"
	ReasonConsentPurposeTooLong    RejectReason = "consent_purpose_too_long"
	ReasonCustomFieldInvalid       RejectReason = "custom_field_invalid"
	ReasonCustomFieldRequired      RejectReason = "custom_field_required"
	ReasonRelativeNumberInvalid    RejectReason = "relative_number_invalid"
	ReasonRelationKindInvalid      RejectReason = "relation_kind_invalid"
	ReasonRelativeNotFound         RejectReason = "relative_not_found"
	ReasonRelationSelf             RejectReason = "relation_self"
	ReasonRelationExists           RejectReason = "relation_exists"
	ReasonContactAmbiguous         RejectReason = "contact_ambiguous"
	ReasonLeadChanging             RejectReason = "lead_changing"
	ReasonRecordInvalid            RejectReason = "record_invalid"
)

type ImportIssue struct {
	Line     int          `json:"line"`
	Reason   RejectReason `json:"reason"`
	Field    string       `json:"field,omitempty"`
	Rejected bool         `json:"rejected"`
}

type ImportPhone struct {
	Number string
	Label  PhoneLabel
}

type ImportRow struct {
	Line           int
	Number         string
	Name           string
	Nickname       string
	Email          string
	BirthDate      string
	Phones         []ImportPhone
	Postal         address.Postal
	Latitude       string
	Longitude      string
	OwnerEmail     string
	ConsentDate    string
	ConsentPurpose string
	CustomFields   map[string]string
	RelativeNumber string
	RelationKind   string
}

type ImportRelative struct {
	Number string
	Kind   RelationKind
}

type ImportRecord struct {
	WorkspaceID  string
	At           time.Time
	Line         int
	Number       string
	Name         string
	Nickname     string
	Email        string
	BirthDate    *shared.Date
	Phones       []ContactPhone
	Address      *Address
	AddressGiven bool
	OwnerEmail   string
	Owner        string
	Consent      *Consent
	CustomFields map[string]any
	Relative     *ImportRelative
}

type PreparedImport struct {
	Records []ImportRecord
	Issues  []ImportIssue
}

func PrepareImport(workspaceID string, rows []ImportRow, defs []*customfield.Definition, at time.Time) PreparedImport {
	p := NewImportPreparer(workspaceID, defs, at)
	prepared := PreparedImport{Records: make([]ImportRecord, 0, len(rows))}
	for _, row := range rows {
		record, issues, ok := p.Prepare(row)
		prepared.Issues = append(prepared.Issues, issues...)
		if ok {
			prepared.Records = append(prepared.Records, record)
		}
	}
	return prepared
}

type ImportPreparer struct {
	workspaceID string
	at          time.Time
	defs        map[string]*customfield.Definition
	seen        map[string]struct{}
	contacts    map[string]struct{}
}

func NewImportPreparer(workspaceID string, defs []*customfield.Definition, at time.Time) *ImportPreparer {
	byKey := make(map[string]*customfield.Definition, len(defs))
	for _, d := range defs {
		if d != nil {
			byKey[d.Key] = d
		}
	}
	return &ImportPreparer{workspaceID: strings.TrimSpace(workspaceID), at: at.UTC(), defs: byKey, seen: map[string]struct{}{}, contacts: map[string]struct{}{}}
}

type rowIssues struct {
	line   int
	issues []ImportIssue
}

func (r *rowIssues) add(reason RejectReason, field string) {
	r.issues = append(r.issues, ImportIssue{Line: r.line, Reason: reason, Field: field})
}

func (r *rowIssues) reject(reason RejectReason, field string) (ImportRecord, []ImportIssue, bool) {
	return ImportRecord{}, append(r.issues, ImportIssue{Line: r.line, Reason: reason, Field: field, Rejected: true}), false
}

func (p *ImportPreparer) Prepare(row ImportRow) (ImportRecord, []ImportIssue, bool) {
	report := &rowIssues{line: row.Line}
	record := ImportRecord{WorkspaceID: p.workspaceID, At: p.at, Line: row.Line}

	if raw := strings.TrimSpace(row.Number); raw != "" {
		number, err := shared.ParsePhone(raw)
		if err != nil {
			return report.reject(ReasonInvalid, ImportFieldNumber)
		}
		if p.alreadySeen(number) {
			return report.reject(ReasonDuplicate, ImportFieldNumber)
		}
		record.Number = number
	}

	record.Name = NormalizeName(row.Name)
	if ValidateName(record.Name) != nil {
		record.Name = ""
		report.add(ReasonNameTooLong, ImportFieldName)
	}
	if record.Number == "" && (&Lead{Name: record.Name}).RealName() == "" {
		return report.reject(ReasonIdentityRequired, ImportFieldName)
	}
	if record.Number != "" {
		p.seen[record.Number] = struct{}{}
	}

	p.prepareContact(row, &record, report)
	if p.repeatsAContact(record) {
		return report.reject(ReasonDuplicate, ImportFieldName)
	}
	p.prepareAddress(row, &record, report)
	p.prepareConsent(row, &record, report)
	p.prepareCustomFields(row, &record, report)
	p.prepareRelative(row, &record, report)
	return record, report.issues, true
}

func (p *ImportPreparer) repeatsAContact(record ImportRecord) bool {
	if record.Number != "" || len(record.Phones) == 0 {
		return false
	}
	name := shared.FoldForMatch(record.Name)
	keys := make([]string, 0, len(record.Phones)*2)
	for _, phone := range record.Phones {
		for _, format := range NumberFormats(phone.Number) {
			keys = append(keys, name+"\x1f"+format)
		}
	}
	for _, key := range keys {
		if _, ok := p.contacts[key]; ok {
			return true
		}
	}
	for _, key := range keys {
		p.contacts[key] = struct{}{}
	}
	return false
}

func (p *ImportPreparer) alreadySeen(number string) bool {
	for _, format := range NumberFormats(number) {
		if _, ok := p.seen[format]; ok {
			return true
		}
	}
	return false
}

func (p *ImportPreparer) prepareContact(row ImportRow, record *ImportRecord, report *rowIssues) {
	if nickname := NormalizeName(row.Nickname); nickname != "" {
		if utf8.RuneCountInString(nickname) > MaxLeadNameLength {
			report.add(ReasonNicknameTooLong, ImportFieldNickname)
		} else {
			record.Nickname = nickname
		}
	}
	if email := strings.ToLower(strings.TrimSpace(row.Email)); email != "" {
		if ValidateEmail(email) != nil {
			report.add(ReasonEmailInvalid, ImportFieldEmail)
		} else {
			record.Email = email
		}
	}
	if raw := strings.TrimSpace(row.BirthDate); raw != "" {
		date, err := shared.ParseLocalDate(raw)
		if err != nil || !birthDateAllowed(date, p.at) {
			report.add(ReasonBirthDateInvalid, ImportFieldBirthDate)
		} else {
			record.BirthDate = &date
		}
	}
	held := &Lead{Number: record.Number}
	for _, phone := range row.Phones {
		if strings.TrimSpace(phone.Number) == "" {
			continue
		}
		number, err := shared.ParsePhone(phone.Number)
		if err != nil || !phone.Label.Valid() {
			report.add(ReasonPhoneInvalid, ImportFieldPhones)
			continue
		}
		if held.HoldsNumber(number) {
			continue
		}
		if len(held.Phones) == MaxImportPhones {
			report.add(ReasonPhoneLimit, ImportFieldPhones)
			continue
		}
		held.Phones = append(held.Phones, ContactPhone{Number: number, Label: phone.Label})
	}
	record.Phones = held.Phones
	if email := strings.ToLower(strings.TrimSpace(row.OwnerEmail)); email != "" {
		if ValidateEmail(email) != nil {
			report.add(ReasonOwnerNotFound, ImportFieldOwner)
		} else {
			record.OwnerEmail = email
		}
	}
}

func (p *ImportPreparer) prepareAddress(row ImportRow, record *ImportRecord, report *rowIssues) {
	postal := row.Postal.Normalize()
	lat, lng := strings.TrimSpace(row.Latitude), strings.TrimSpace(row.Longitude)
	record.AddressGiven = postal != (address.Postal{}) || lat != "" || lng != ""
	if postal == (address.Postal{}) {
		if lat != "" || lng != "" {
			report.add(ReasonAddressInvalid, ImportFieldAddress)
		}
		return
	}
	if postal.Validate() != nil {
		report.add(ReasonAddressInvalid, ImportFieldAddress)
		return
	}
	a := &Address{Label: AddressHome, Primary: true, Postal: postal, GeoStatus: GeoPending}
	record.Address = a
	if lat == "" && lng == "" {
		return
	}
	point, ok := parsePoint(lat, lng)
	if !ok {
		report.add(ReasonCoordinatesInvalid, ImportFieldCoordinates)
		return
	}
	if !point.InBrazil() {
		report.add(ReasonCoordinatesOutsideBrazil, ImportFieldCoordinates)
		return
	}
	a.Fix = &geo.Fix{Point: point, Precision: geo.PrecisionExact, Source: geo.SourceImport, FixedAt: p.at}
	a.GeoStatus = StatusOfFix(*a.Fix)
}

func parsePoint(lat, lng string) (geo.Point, bool) {
	latitude, err := shared.ParseDecimal(lat)
	if err != nil {
		return geo.Point{}, false
	}
	longitude, err := shared.ParseDecimal(lng)
	if err != nil {
		return geo.Point{}, false
	}
	point := geo.Point{Lat: latitude, Lng: longitude}
	return point, point.Validate() == nil
}

func (p *ImportPreparer) prepareConsent(row ImportRow, record *ImportRecord, report *rowIssues) {
	raw, purpose := strings.TrimSpace(row.ConsentDate), strings.Join(strings.Fields(row.ConsentPurpose), " ")
	if raw == "" {
		if purpose != "" {
			report.add(ReasonConsentDateInvalid, ImportFieldConsentDate)
		}
		return
	}
	date, err := shared.ParseLocalDate(raw)
	if err != nil || date.After(p.at) {
		report.add(ReasonConsentDateInvalid, ImportFieldConsentDate)
		return
	}
	if utf8.RuneCountInString(purpose) > MaxConsentPurposeLength {
		report.add(ReasonConsentPurposeTooLong, ImportFieldConsentPurpose)
		return
	}
	granted := time.Date(date.Year, date.Month, date.Day, 12, 0, 0, 0, time.UTC)
	record.Consent = &Consent{GrantedAt: granted, Source: ConsentImport, Purpose: purpose}
}

func (p *ImportPreparer) prepareCustomFields(row ImportRow, record *ImportRecord, report *rowIssues) {
	for key, raw := range row.CustomFields {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := p.defs[key].CoerceLocal(raw)
		if err != nil {
			report.add(ReasonCustomFieldInvalid, ImportCustomField(key))
			continue
		}
		if record.CustomFields == nil {
			record.CustomFields = map[string]any{}
		}
		record.CustomFields[key] = value
	}
}

func (p *ImportPreparer) prepareRelative(row ImportRow, record *ImportRecord, report *rowIssues) {
	raw := strings.TrimSpace(row.RelativeNumber)
	if raw == "" {
		return
	}
	number, err := shared.ParsePhone(raw)
	if err != nil {
		report.add(ReasonRelativeNumberInvalid, ImportFieldRelative)
		return
	}
	kind, ok := ParseRelationKind(row.RelationKind)
	if !ok {
		report.add(ReasonRelationKindInvalid, ImportFieldRelationKind)
		return
	}
	if record.Number != "" && sameNumber(record.Number, number) {
		report.add(ReasonRelationSelf, ImportFieldRelative)
		return
	}
	record.Relative = &ImportRelative{Number: number, Kind: kind}
}
