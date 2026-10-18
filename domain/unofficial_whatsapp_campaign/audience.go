package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
)

var (
	ErrCampaignAudienceInvalid   = errors.New("campaign audience must be leads or groups")
	ErrCampaignAudienceMismatch  = errors.New("this campaign sends to groups; add groups, not numbers")
	ErrGroupCampaignNumberLocked = errors.New("a group campaign sends from the number that is in those groups; create a new campaign to use another number")
	ErrGroupCampaignFunnel       = errors.New("a group campaign posts into the group conversation, which stays in the number's funnel; leave the funnel empty")
)

type Audience string

const (
	AudienceLeads  Audience = "leads"
	AudienceGroups Audience = "groups"
)

func (a Audience) Valid() bool {
	return a == AudienceLeads || a == AudienceGroups
}

func (c *Campaign) TargetsGroups() bool {
	return c.Audience == AudienceGroups
}

func normalizeGroupTargets(targets []TargetInput) []TargetInput {
	seen := make(map[string]struct{}, len(targets))
	clean := make([]TargetInput, 0, len(targets))
	for _, t := range targets {
		t.GroupJID = strings.TrimSpace(t.GroupJID)
		t.Name = strings.TrimSpace(t.Name)
		if t.GroupJID != "" {
			if _, dup := seen[t.GroupJID]; dup {
				continue
			}
			seen[t.GroupJID] = struct{}{}
		}
		clean = append(clean, t)
	}
	return clean
}

func validateTarget(audience Audience, t TargetInput) error {
	if audience == AudienceGroups {
		if t.LeadID != "" || t.Number != "" || !uw.IsGroupJID(t.GroupJID) {
			return fmt.Errorf("%w: %q", ErrCampaignTargetInvalid, firstNonEmptyTarget(t))
		}
		return nil
	}
	if t.GroupJID != "" {
		return fmt.Errorf("%w: %q", ErrCampaignTargetInvalid, t.GroupJID)
	}
	if t.LeadID == "" && !ValidTargetNumber(t.Number) {
		return fmt.Errorf("%w: %q", ErrCampaignTargetInvalid, t.Number)
	}
	return nil
}

func firstNonEmptyTarget(t TargetInput) string {
	for _, v := range []string{t.GroupJID, t.Number, t.LeadID} {
		if v != "" {
			return v
		}
	}
	return ""
}

func NewGroupEntry(campaignID, workspaceID, groupJID, name string, now time.Time) Entry {
	checked := now.UTC()
	return Entry{
		CampaignID:  campaignID,
		WorkspaceID: workspaceID,
		JID:         strings.TrimSpace(groupJID),
		Name:        strings.TrimSpace(name),
		CheckedAt:   &checked,
		Status:      campaign.SendStatusPending,
	}
}

func (e *Entry) TargetsGroup() bool {
	return e.LeadID == "" && uw.IsGroupJID(e.JID)
}

func (c *Campaign) RefuseNumberChange(edit *Campaign) error {
	if !c.TargetsGroups() || edit == nil || edit.InstanceID == "" || edit.InstanceID == c.InstanceID {
		return nil
	}
	return ErrGroupCampaignNumberLocked
}
