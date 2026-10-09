package lead

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/datatypes"

	"vozko/domain/actor"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func ownerColumns(owner string) (schema.OptionalText, schema.OptionalText) {
	if owner == "" {
		return "", ""
	}
	id, kind := actor.Split(owner)
	return schema.OptionalText(id), schema.OptionalText(kind)
}

func ownerOf(id, kind schema.OptionalText) string {
	if id == "" {
		return ""
	}
	return actor.Join(string(id), actor.Kind(kind))
}

func birthDateColumn(date *shared.Date) schema.CalendarDate {
	if date == nil {
		return ""
	}
	return schema.CalendarDate(date.String())
}

func birthDateOf(id string, column schema.CalendarDate) (*shared.Date, error) {
	if column == "" {
		return nil, nil
	}
	date, err := shared.ParseDate(string(column))
	if err != nil {
		return nil, fmt.Errorf("lead %s birth date %q: %w", id, column, err)
	}
	return &date, nil
}

func customFieldsColumn(values map[string]any) (datatypes.JSON, error) {
	if len(values) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("lead custom fields: %w", err)
	}
	return datatypes.JSON(raw), nil
}

func customFieldsOf(id string, raw datatypes.JSON) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("lead %s custom fields are not an object: %w", id, err)
	}
	return values, nil
}

func consentColumns(c *lead.Consent) (*time.Time, schema.OptionalText, schema.OptionalText) {
	if c == nil {
		return nil, "", ""
	}
	at := c.GrantedAt
	return &at, schema.OptionalText(c.Source), schema.OptionalText(c.Purpose)
}

func consentOf(at *time.Time, source, purpose schema.OptionalText) *lead.Consent {
	if at == nil {
		return nil
	}
	return &lead.Consent{GrantedAt: *at, Source: lead.ConsentSource(source), Purpose: string(purpose)}
}

func blockedAtColumn(l *lead.Lead) *time.Time {
	if !l.Blocked || l.BlockedAt.IsZero() {
		return nil
	}
	at := l.BlockedAt
	return &at
}

func toSchema(l *lead.Lead) (*schema.Lead, error) {
	customFields, err := customFieldsColumn(l.CustomFields)
	if err != nil {
		return nil, err
	}
	ownerID, ownerKind := ownerColumns(l.Owner)
	optInAt, optInSource, optInPurpose := consentColumns(l.WhatsAppOptIn)
	return &schema.Lead{
		ID:                   l.ID,
		WorkspaceID:          l.WorkspaceID,
		Number:               schema.OptionalText(l.Number),
		Name:                 l.Name,
		NameSource:           schema.OptionalText(l.NameSource),
		Nickname:             schema.OptionalText(l.Nickname),
		Email:                schema.OptionalText(l.Email),
		BirthDate:            birthDateColumn(l.BirthDate),
		Source:               schema.OptionalText(l.Source),
		OwnerID:              ownerID,
		OwnerKind:            ownerKind,
		CustomFields:         customFields,
		WhatsAppOptInAt:      optInAt,
		WhatsAppOptInSource:  optInSource,
		WhatsAppOptInPurpose: optInPurpose,
		OptedOutAt:           l.OptedOutAt,
		OptedOutSource:       optOutSourceColumn(l),
		ProfilePictureURL:    l.ProfilePictureURL,
		Age:                  l.StoredAge,
		Blocked:              l.Blocked,
		BlockedAt:            blockedAtColumn(l),
		BlockedBy:            l.BlockedBy,
		RelativesCount:       l.RelativesCount,
		ReferredCount:        l.ReferredCount,
		Version:              l.Version,
		CreatedAt:            l.CreatedAt,
		UpdatedAt:            l.UpdatedAt,
	}, nil
}

func toDomain(row *schema.Lead) (*lead.Lead, error) {
	birthDate, err := birthDateOf(row.ID, row.BirthDate)
	if err != nil {
		return nil, err
	}
	customFields, err := customFieldsOf(row.ID, row.CustomFields)
	if err != nil {
		return nil, err
	}
	l := &lead.Lead{
		ID:                row.ID,
		WorkspaceID:       row.WorkspaceID,
		Number:            string(row.Number),
		Name:              row.Name,
		NameSource:        lead.Source(row.NameSource),
		Nickname:          string(row.Nickname),
		Email:             string(row.Email),
		BirthDate:         birthDate,
		Source:            lead.Source(row.Source),
		Owner:             ownerOf(row.OwnerID, row.OwnerKind),
		CustomFields:      customFields,
		WhatsAppOptIn:     consentOf(row.WhatsAppOptInAt, row.WhatsAppOptInSource, row.WhatsAppOptInPurpose),
		OptedOutAt:        row.OptedOutAt,
		OptOutSource:      optOutSourceOf(row),
		ProfilePictureURL: row.ProfilePictureURL,
		StoredAge:         row.Age,
		Blocked:           row.Blocked,
		BlockedBy:         row.BlockedBy,
		RelativesCount:    row.RelativesCount,
		ReferredCount:     row.ReferredCount,
		Version:           row.Version,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
	if row.BlockedAt != nil {
		l.BlockedAt = *row.BlockedAt
	}
	return l, nil
}

func toDomainAll(rows []schema.Lead) ([]*lead.Lead, error) {
	leads := make([]*lead.Lead, len(rows))
	for i := range rows {
		l, err := toDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		leads[i] = l
	}
	return leads, nil
}

func optOutSourceColumn(l *lead.Lead) schema.OptionalText {
	if l.OptedOutAt == nil {
		return ""
	}
	return schema.OptionalText(l.OptOutSource)
}

func optOutSourceOf(row *schema.Lead) lead.OptOutSource {
	if row.OptedOutAt == nil {
		return ""
	}
	return lead.OptOutSource(row.OptedOutSource)
}
