package lead

import (
	"time"

	"vozko/domain/shared"
)

type LeadDigest struct {
	LeadID         string `json:"lead_id"`
	Name           string `json:"name,omitempty"`
	Contact        string `json:"contact,omitempty"`
	Blocked        bool   `json:"blocked"`
	OptedOut       bool   `json:"opted_out,omitempty"`
	Age            *int   `json:"age,omitempty"`
	District       string `json:"district,omitempty"`
	City           string `json:"city,omitempty"`
	State          string `json:"state,omitempty"`
	Owner          string `json:"owner,omitempty"`
	Campaigns      int    `json:"campaigns"`
	Memories       int    `json:"memories"`
	WindowOpen     bool   `json:"window_open"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func Digest(l *Lead, s *LeadSummary) LeadDigest {
	d := LeadDigest{
		LeadID:    l.ID,
		Name:      l.RealName(),
		Contact:   shared.MaskContact(l.Number),
		Blocked:   l.Blocked,
		OptedOut:  l.OptedOutAt != nil,
		Age:       l.StoredAge,
		CreatedAt: digestTime(&l.CreatedAt),
	}
	if primary := l.PrimaryAddress(); primary != nil {
		d.District, d.City, d.State = primary.Postal.District, primary.Postal.City, primary.Postal.State
	}
	if s != nil {
		d.Campaigns = s.TotalCampaigns
		d.Memories = s.Memories
		d.WindowOpen = s.WhatsAppWindowOpen
		d.LastActivityAt = digestTime(s.LastActivityAt)
	}
	return d
}

func DigestItem(item *LeadWithSummary) LeadDigest {
	d := Digest(item.Lead, item.Summary)
	d.Owner = item.OwnerName
	return d
}

func digestTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
