package lead

import (
	"testing"
	"time"
)

func TestDigestHidesTheNumberAndCarriesTheSummary(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	age := 34
	d := Digest(
		&Lead{ID: "l1", Name: "Maria", Number: "5584994409624", Age: &age, CreatedAt: at},
		&LeadSummary{TotalCampaigns: 3, Memories: 2, LastActivityAt: &at, WhatsAppWindowOpen: true},
	)
	if d.Contact != "••••9624" {
		t.Fatalf("contact = %q", d.Contact)
	}
	if d.LeadID != "l1" || d.Name != "Maria" || *d.Age != 34 || d.Campaigns != 3 || d.Memories != 2 ||
		!d.WindowOpen || d.LastActivityAt != "2026-09-20T14:30:00Z" || d.CreatedAt != "2026-09-20T14:30:00Z" {
		t.Fatalf("digest = %+v", d)
	}
}

func TestDigestWithoutASummary(t *testing.T) {
	d := Digest(&Lead{ID: "l1", Blocked: true}, nil)
	if d.LeadID != "l1" || !d.Blocked || d.Campaigns != 0 || d.LastActivityAt != "" {
		t.Fatalf("digest = %+v", d)
	}
}
