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
	Age            *int   `json:"age,omitempty"`
	Campaigns      int    `json:"campaigns"`
	Memories       int    `json:"memories"`
	WindowOpen     bool   `json:"window_open"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

func Digest(l *Lead, s *LeadSummary) LeadDigest {
	d := LeadDigest{
		LeadID:    l.ID,
		Name:      l.Name,
		Contact:   shared.MaskContact(l.Number),
		Blocked:   l.Blocked,
		Age:       l.Age,
		CreatedAt: digestTime(&l.CreatedAt),
	}
	if s != nil {
		d.Campaigns = s.TotalCampaigns
		d.Memories = s.Memories
		d.WindowOpen = s.WhatsAppWindowOpen
		d.LastActivityAt = digestTime(s.LastActivityAt)
	}
	return d
}

func digestTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
