package quality

import (
	"errors"
	"testing"
	"time"
)

func validChange() Change {
	return Change{
		Subject: SubjectTemplate, ExternalID: "123", Signal: SignalQuality, Source: SourceWebhook,
		Current: "RED", OccurredAt: time.Date(2026, 10, 15, 13, 34, 38, 0, time.UTC),
	}
}

func TestChangeValidateRefusesWhatCannotBeMeasured(t *testing.T) {
	cases := map[string]func(*Change){
		"no subject":     func(c *Change) { c.Subject = "" },
		"no external id": func(c *Change) { c.ExternalID = " " },
		"no signal":      func(c *Change) { c.Signal = "" },
		"no source":      func(c *Change) { c.Source = "" },
		"no current":     func(c *Change) { c.Current = "" },
		"no moment":      func(c *Change) { c.OccurredAt = time.Time{} },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			c := validChange()
			break_(&c)
			if !errors.Is(c.Validate(), ErrInvalidChange) {
				t.Fatalf("want ErrInvalidChange, got %v", c.Validate())
			}
		})
	}
	if err := validChange().Validate(); err != nil {
		t.Fatalf("a complete change is valid, got %v", err)
	}
}

func TestChangeMovedOnlyWhenTheValueChanged(t *testing.T) {
	base := Change{Subject: SubjectPhone, ExternalID: "p-1", Signal: SignalMessagingLimit, Source: SourceWebhook}
	if _, moved := base.Moved("TIER_1K", " TIER_1K "); moved {
		t.Fatal("the same value is not a change")
	}
	if _, moved := base.Moved("TIER_1K", ""); moved {
		t.Fatal("an empty reading is not a change")
	}
	got, moved := base.Moved("TIER_1K", "TIER_250")
	if !moved || got.Previous != "TIER_1K" || got.Current != "TIER_250" || got.ExternalID != "p-1" {
		t.Fatalf("got %+v, %v", got, moved)
	}
}

func TestOccurredAtPrefersTheMomentMetaReports(t *testing.T) {
	fallback := time.Date(2026, 10, 15, 16, 0, 0, 0, time.UTC)
	if got := OccurredAt(1791990878, fallback); !got.Equal(time.Unix(1791990878, 0)) {
		t.Fatalf("got %v", got)
	}
	if got := OccurredAt(0, fallback); !got.Equal(fallback) {
		t.Fatalf("without Meta's moment it falls back to now, got %v", got)
	}
}
