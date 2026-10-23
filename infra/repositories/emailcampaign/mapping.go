package emailcampaign

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/datatypes"

	domain "vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/selectionstore"
)

func encodeSelection(s *selection.Selection) (datatypes.JSON, error) {
	if s == nil {
		return nil, nil
	}
	return selectionstore.Encode(*s)
}

func decodeSelection(raw datatypes.JSON) (*selection.Selection, error) {
	if empty(raw) {
		return nil, nil
	}
	s, err := selectionstore.Decode(raw)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func empty(raw datatypes.JSON) bool {
	return len(raw) == 0 || string(raw) == "null"
}

func encodeOptional(v any, present bool) (datatypes.JSON, error) {
	if !present {
		return nil, nil
	}
	return json.Marshal(v)
}

func decodeInto[T any](raw datatypes.JSON) (*T, error) {
	if empty(raw) {
		return nil, nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func toRow(c *domain.Campaign) (schema.EmailCampaign, error) {
	doc, err := json.Marshal(c.Document)
	if err != nil {
		return schema.EmailCampaign{}, err
	}
	sel, err := encodeSelection(c.Selection)
	if err != nil {
		return schema.EmailCampaign{}, err
	}
	policies, err := encodeOptional(c.MergePolicies, len(c.MergePolicies) > 0)
	if err != nil {
		return schema.EmailCampaign{}, err
	}
	ab, err := encodeOptional(c.ABTest, c.ABTest != nil)
	if err != nil {
		return schema.EmailCampaign{}, err
	}
	utm, err := encodeOptional(c.UTM, c.UTM != nil)
	if err != nil {
		return schema.EmailCampaign{}, err
	}
	return schema.EmailCampaign{
		ID:             c.ID,
		WorkspaceID:    c.WorkspaceID,
		Name:           c.Name,
		Status:         string(c.Status),
		HoldReason:     string(c.HoldReason),
		SenderID:       schema.OptionalText(c.SenderID),
		Subject:        c.Subject,
		Preheader:      c.Preheader,
		Document:       doc,
		Selection:      sel,
		ListID:         schema.OptionalText(c.ListID),
		MergePolicies:  policies,
		ABTest:         ab,
		UTM:            utm,
		ScheduledAt:    c.ScheduledAt,
		StartedAt:      c.StartedAt,
		CompletedAt:    c.CompletedAt,
		RefundedUnits:  settlementOf(c).RefundedUnits,
		RefundedMicros: settlementOf(c).RefundedMicros,
		SettledAt:      settledAt(c),
		CreatedBy:      schema.OptionalText(c.CreatedBy),
		LaunchedBy:     schema.OptionalText(c.LaunchedBy),
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}, nil
}

func toDomain(row schema.EmailCampaign) (domain.Campaign, error) {
	c := domain.Campaign{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Status:      domain.Status(row.Status),
		HoldReason:  domain.HoldReason(row.HoldReason),
		SenderID:    string(row.SenderID),
		Subject:     row.Subject,
		Preheader:   row.Preheader,
		ListID:      string(row.ListID),
		ScheduledAt: utc(row.ScheduledAt),
		StartedAt:   utc(row.StartedAt),
		CompletedAt: utc(row.CompletedAt),
		CreatedBy:   string(row.CreatedBy),
		LaunchedBy:  string(row.LaunchedBy),
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
	if row.SettledAt != nil {
		c.Settlement = &domain.RunSettlement{RefundedUnits: row.RefundedUnits, RefundedMicros: row.RefundedMicros, SettledAt: row.SettledAt.UTC()}
	}
	if state := domain.TestBatchState(row.TestBatchState); state != domain.TestBatchNone {
		if !state.Valid() {
			return domain.Campaign{}, fmt.Errorf("emailcampaign: campaign %s has the unknown test batch state %q", row.ID, row.TestBatchState)
		}
		c.TestBatch = &domain.TestBatch{Size: row.TestBatchSize, State: state, LastID: row.TestBatchLastID, DrainedAt: utc(row.TestBatchDrainedAt), DecidedAt: utc(row.TestBatchDecidedAt)}
	}
	if !empty(row.Document) {
		if err := json.Unmarshal(row.Document, &c.Document); err != nil {
			return domain.Campaign{}, err
		}
	}
	var err error
	if c.Selection, err = decodeSelection(row.Selection); err != nil {
		return domain.Campaign{}, err
	}
	if !empty(row.MergePolicies) {
		if err := json.Unmarshal(row.MergePolicies, &c.MergePolicies); err != nil {
			return domain.Campaign{}, err
		}
	}
	if c.ABTest, err = decodeInto[domain.ABTest](row.ABTest); err != nil {
		return domain.Campaign{}, err
	}
	if c.UTM, err = decodeInto[emaildesign.UTM](row.UTM); err != nil {
		return domain.Campaign{}, err
	}
	return c, nil
}

func settlementOf(c *domain.Campaign) domain.RunSettlement {
	if c.Settlement == nil {
		return domain.RunSettlement{}
	}
	return *c.Settlement
}

func settledAt(c *domain.Campaign) *time.Time {
	if c.Settlement == nil {
		return nil
	}
	at := c.Settlement.SettledAt.UTC()
	return &at
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}
