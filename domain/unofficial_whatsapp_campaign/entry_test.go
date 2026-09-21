package unofficial_whatsapp_campaign

import (
	"testing"
	"time"

	"vozko/domain/campaign"
)

func TestStatusSetIncludesTheSkipBucketAsNonDispatch(t *testing.T) {
	set := StatusSet()
	if !set.Valid(campaign.SendStatusSkippedNotOnWhatsApp) {
		t.Fatal("channel cannot produce its own skip status")
	}
	for _, s := range set.Dispatched() {
		if s == campaign.SendStatusSkippedNotOnWhatsApp {
			t.Fatal("a number that is not on WhatsApp was counted as a dispatch")
		}
	}
}

func TestSkippedIsNotCountedAsFailed(t *testing.T) {
	counts := &campaign.Counts{Total: 10, Sent: 7, SkippedNotOnWhatsApp: 3}
	m := campaign.NewMetrics(counts)
	if m.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", m.Failed)
	}
	if m.SkippedNotOnWhatsApp != 3 {
		t.Fatalf("SkippedNotOnWhatsApp = %d, want 3", m.SkippedNotOnWhatsApp)
	}
	if m.Dispatches != 7 {
		t.Fatalf("Dispatches = %d, want 7", m.Dispatches)
	}
}

func TestEntryNormalizeAndValidate(t *testing.T) {
	e := &Entry{CampaignID: " c1 ", LeadID: " l1 ", Number: "+55 (84) 99999-0001"}
	e.Normalize()
	if e.CampaignID != "c1" || e.LeadID != "l1" {
		t.Fatalf("ids not trimmed: %+v", e)
	}
	if e.Number != "5584999990001" {
		t.Fatalf("number = %q, want normalized digits", e.Number)
	}
	if e.Status != campaign.SendStatusPending {
		t.Fatalf("status = %q, want PENDING", e.Status)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}

	e.LeadID = ""
	if err := e.Validate(); err == nil {
		t.Fatal("entry without a lead accepted")
	}
}

func TestNeedsNumberCheck(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour)
	stale := now.Add(-NumberCheckTTL - time.Hour)

	cases := map[string]struct {
		entry Entry
		want  bool
	}{
		"never checked":    {Entry{}, true},
		"jid but no clock": {Entry{JID: "x@s.whatsapp.net"}, true},
		"clock but no jid": {Entry{CheckedAt: &fresh}, true},
		"fresh":            {Entry{JID: "x@s.whatsapp.net", CheckedAt: &fresh}, false},
		"stale":            {Entry{JID: "x@s.whatsapp.net", CheckedAt: &stale}, true},
	}
	for name, c := range cases {
		if got := c.entry.NeedsNumberCheck(now); got != c.want {
			t.Errorf("%s: NeedsNumberCheck = %v, want %v", name, got, c.want)
		}
	}
}

func TestUnknownStatusIsRepairedNotAccepted(t *testing.T) {
	e := &Entry{CampaignID: "c", LeadID: "l", Status: campaign.SendStatus("WAT")}
	if ValidStatus(e.Status) {
		t.Fatal("an unknown status validated")
	}
	e.Normalize()
	if e.Status != campaign.SendStatusPending {
		t.Fatalf("status = %q, want PENDING after repair", e.Status)
	}
}
