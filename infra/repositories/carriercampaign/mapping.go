package carriercampaign_repository

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/selectionstore"
)

var errUnknownStoredValue = errors.New("carriercampaign: unknown stored value")

type validated interface {
	~string
	Valid() bool
}

func valid[T validated](field string, value T) error {
	if value.Valid() {
		return nil
	}
	return fmt.Errorf("%w: %s %q", errUnknownStoredValue, field, value)
}

func validOrEmpty[T validated](field string, value T) error {
	if value == "" {
		return nil
	}
	return valid(field, value)
}

func newID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func wellFormedID(id string) bool {
	return len(database.UUIDArray([]string{id})) == 1
}

func encodeJSON(field string, v any) (datatypes.JSON, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("carriercampaign: encoding %s: %w", field, err)
	}
	return datatypes.JSON(raw), nil
}

func decodeJSON(field string, raw datatypes.JSON, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("carriercampaign: decoding stored %s: %w", field, err)
	}
	return nil
}

func validCampaign(c *carriercampaign.Campaign) error {
	return errors.Join(
		valid("campaign status", c.Status),
		valid("campaign kind", c.Kind),
		valid("channel", c.Channel),
	)
}

func toCampaignRow(c *carriercampaign.Campaign) (*schema.CarrierCampaign, error) {
	if err := validCampaign(c); err != nil {
		return nil, err
	}
	variants, err := encodeJSON("variants", c.Variants)
	if err != nil {
		return nil, err
	}
	fallback, err := encodeJSON("fallback", c.Fallback)
	if err != nil {
		return nil, err
	}
	raw, err := selectionstore.Encode(c.Audience)
	if err != nil {
		return nil, fmt.Errorf("carriercampaign: encoding audience: %w", err)
	}
	audience := datatypes.JSON(raw)
	var recurrence datatypes.JSON
	if c.Recurrence != nil {
		if recurrence, err = encodeJSON("recurrence", c.Recurrence); err != nil {
			return nil, err
		}
	}
	return &schema.CarrierCampaign{
		ID:          c.ID,
		WorkspaceID: c.WorkspaceID,
		Name:        c.Name,
		SenderID:    c.SenderID,
		Channel:     string(c.Channel),
		Status:      string(c.Status),
		Kind:        string(c.Kind),
		Variants:    variants,
		Fallback:    fallback,
		Audience:    audience,
		ScheduledAt: c.ScheduledAt,
		Recurrence:  recurrence,
		CreatedBy:   schema.OptionalText(c.CreatedBy),
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}, nil
}

func toCampaign(r *schema.CarrierCampaign) (*carriercampaign.Campaign, error) {
	c := &carriercampaign.Campaign{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		SenderID:    r.SenderID,
		Channel:     carriermessaging.Channel(r.Channel),
		Status:      carriercampaign.Status(r.Status),
		Kind:        carriercampaign.Kind(r.Kind),
		ScheduledAt: r.ScheduledAt,
		CreatedBy:   string(r.CreatedBy),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if err := validCampaign(c); err != nil {
		return nil, fmt.Errorf("carrier campaign %s: %w", r.ID, err)
	}
	if err := decodeJSON("variants", r.Variants, &c.Variants); err != nil {
		return nil, err
	}
	if err := decodeJSON("fallback", r.Fallback, &c.Fallback); err != nil {
		return nil, err
	}
	audience, err := selectionstore.Decode(r.Audience)
	if err != nil {
		return nil, fmt.Errorf("carriercampaign: decoding audience: %w", err)
	}
	c.Audience = audience
	if len(r.Recurrence) > 0 && string(r.Recurrence) != "null" {
		c.Recurrence = &carriercampaign.Recurrence{}
		if err := decodeJSON("recurrence", r.Recurrence, c.Recurrence); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func toCampaigns(rows []schema.CarrierCampaign) ([]*carriercampaign.Campaign, error) {
	out := make([]*carriercampaign.Campaign, 0, len(rows))
	for i := range rows {
		c, err := toCampaign(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
