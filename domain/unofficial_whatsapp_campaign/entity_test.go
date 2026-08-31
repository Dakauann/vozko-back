package unofficial_whatsapp_campaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
)

func validCampaign() *Campaign {
	return &Campaign{
		WorkspaceID: "ws-1",
		InstanceID:  "inst-1",
		Name:        "Cobrança agosto",
		Message:     MessageSpec{Kind: KindText, Bodies: []string{"bom dia"}},
		Targets:     []TargetInput{{Number: "5584999990001"}},
	}
}

func TestValidateRequiresNameInstanceAndTargets(t *testing.T) {
	c := validCampaign()
	c.Name = ""
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignNameRequired) {
		t.Errorf("no name: got %v", err)
	}

	c = validCampaign()
	c.InstanceID = ""
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignInstanceIDRequired) {
		t.Errorf("no instance: got %v", err)
	}

	c = validCampaign()
	c.Targets = nil
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignTargetsRequired) {
		t.Errorf("no targets: got %v", err)
	}
}

// The same person written two ways is one person. Sending them a campaign twice
// is the single most common cause of a ban complaint.
func TestNormalizeDedupsByNormalizedNumber(t *testing.T) {
	c := validCampaign()
	c.Targets = []TargetInput{
		{Number: "+55 84 99999-0001"},
		{Number: "5584999990001"},
		{Number: "(84) 99999-0002"},
	}
	c.Normalize()
	if len(c.Targets) != 2 {
		t.Fatalf("targets = %d, want 2 after dedup: %+v", len(c.Targets), c.Targets)
	}
	if c.Targets[0].Number != "5584999990001" {
		t.Fatalf("first target = %q, want normalized digits", c.Targets[0].Number)
	}
}

// Unlike the official campaign this channel is not Brazil-pinned: it connects
// the customer's own handset and is used to reach numbers anywhere.
func TestTargetNumbersAreNotCountryPinned(t *testing.T) {
	cases := map[string]bool{
		"5584999990001":    true,  // BR
		"351912345678":     true,  // PT
		"12025550123":      true,  // US
		"123":              false, // too short
		"1234567890123456": false, // past E.164
		"":                 false,
	}
	for number, want := range cases {
		if got := ValidTargetNumber(number); got != want {
			t.Errorf("ValidTargetNumber(%q) = %v, want %v", number, got, want)
		}
	}
}

func TestTargetsCapEnforced(t *testing.T) {
	c := validCampaign()
	c.Targets = make([]TargetInput, MaxCampaignTargets+1)
	for i := range c.Targets {
		// Distinct numbers, or Normalize would dedup them below the cap.
		c.Targets[i] = TargetInput{Number: "55849" + padded(i)}
	}
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignTargetsTooMany) {
		t.Fatalf("err = %v, want ErrCampaignTargetsTooMany", err)
	}
}

func padded(i int) string {
	s := ""
	for n := i; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	for len(s) < 8 {
		s = "0" + s
	}
	return s
}

// Defaulting only the minimum and then clamping would collapse an unset range to
// min..min — a FIXED cadence, which is the machine-regular rhythm the jitter
// exists to avoid.
func TestUnsetDelaysDoNotCollapseToAFixedCadence(t *testing.T) {
	c := validCampaign()
	c.Normalize()
	min, max := c.SendDelayRange()
	if min == max {
		t.Fatalf("unset delays collapsed to a fixed %dms cadence", min)
	}
	if min != uw.DefaultSendDelayMinMS || max != uw.DefaultSendDelayMaxMS {
		t.Fatalf("delays = %d..%d, want the channel defaults", min, max)
	}
}

// An operator must not be able to configure a campaign faster than the channel
// floor, whatever they put in the form.
func TestDelaysClampToTheChannelFloor(t *testing.T) {
	c := validCampaign()
	c.SendDelayMinMS = 1
	c.SendDelayMaxMS = 2
	c.Normalize()
	min, _ := c.SendDelayRange()
	if min < uw.MinSendDelayMS {
		t.Fatalf("min delay = %d, below the floor of %d", min, uw.MinSendDelayMS)
	}
}

func TestInvertedDelayRangeIsRepaired(t *testing.T) {
	c := validCampaign()
	c.SendDelayMinMS = 9000
	c.SendDelayMaxMS = 1000
	c.Normalize()
	min, max := c.SendDelayRange()
	if max < min {
		t.Fatalf("range still inverted: %d..%d", min, max)
	}
}

// The effective cap is always the lower of the two, and a campaign cap of zero
// means "no campaign limit" — never "no limit", because the ban risk belongs to
// the number.
func TestEffectiveDailyCapTakesTheLower(t *testing.T) {
	cases := []struct {
		campaignCap, instanceCap, want int
	}{
		{0, 1000, 1000},
		{500, 1000, 500},
		{2000, 1000, 1000},
		{500, 0, 500},
		{0, 0, 0},
	}
	for _, c := range cases {
		camp := &Campaign{DailyCap: c.campaignCap}
		if got := camp.EffectiveDailyCap(c.instanceCap); got != c.want {
			t.Errorf("campaign %d + instance %d = %d, want %d",
				c.campaignCap, c.instanceCap, got, c.want)
		}
	}
}

func TestScheduleBounds(t *testing.T) {
	c := validCampaign()
	c.ScheduledStart = time.Now().Add(-time.Hour)
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignScheduledStartInvalid) {
		t.Errorf("past start: got %v", err)
	}

	c = validCampaign()
	c.ScheduledStart = time.Now().Add(time.Minute)
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignScheduledStartTooSoon) {
		t.Errorf("too soon: got %v", err)
	}

	c = validCampaign()
	c.ScheduledStart = time.Now().Add(2 * 365 * 24 * time.Hour)
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrCampaignScheduledStartTooSoon) {
		t.Errorf("too far: got %v", err)
	}

	c = validCampaign()
	c.ScheduledStart = time.Now().Add(time.Hour)
	c.Normalize()
	if err := c.Validate(); err != nil {
		t.Errorf("an hour out was rejected: %v", err)
	}
}

func TestValidateTargetVariables(t *testing.T) {
	c := validCampaign()
	c.Targets = []TargetInput{{Number: "5584999990001", Variables: []string{"Ana"}}}
	c.Normalize()

	if err := c.ValidateTargetVariables(2); !errors.Is(err, ErrCampaignVariablesMismatch) {
		t.Errorf("too few variables: got %v", err)
	}
	if err := c.ValidateTargetVariables(1); err != nil {
		t.Errorf("exact count rejected: %v", err)
	}

	c.Targets[0].Variables = []string{"  "}
	if err := c.ValidateTargetVariables(1); !errors.Is(err, ErrCampaignVariableEmpty) {
		t.Errorf("blank variable: got %v", err)
	}
}

func TestValidateWorkflowVars(t *testing.T) {
	c := validCampaign()
	c.Targets = []TargetInput{{Number: "5584999990001", Metadata: map[string]interface{}{"cpf": "123"}}}
	c.Normalize()

	if err := c.ValidateWorkflowVars([]string{"cpf"}); err != nil {
		t.Errorf("present key rejected: %v", err)
	}
	if err := c.ValidateWorkflowVars([]string{"valor"}); !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Errorf("missing key: got %v", err)
	}
	c.Targets[0].Metadata["valor"] = "   "
	if err := c.ValidateWorkflowVars([]string{"valor"}); !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Errorf("blank key: got %v", err)
	}
}

// A status column that is somehow empty must not read as permission to send.
func TestEmptyStatusNormalizesToStopped(t *testing.T) {
	c := validCampaign()
	c.Normalize()
	if c.Status != campaign.StatusStopped {
		t.Fatalf("status = %q, want STOPPED", c.Status)
	}
}
