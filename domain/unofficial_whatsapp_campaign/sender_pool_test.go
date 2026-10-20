package unofficial_whatsapp_campaign

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAGroupCampaignTakesNoNumber(t *testing.T) {
	c := groupCampaign(TargetInput{GroupJID: "120363153742561022@g.us"})
	c.InstanceID = "n1"
	c.Normalize()
	if err := c.Validate(); !errors.Is(err, ErrGroupCampaignTakesNoNumber) {
		t.Fatalf("Validate with a number = %v, want ErrGroupCampaignTakesNoNumber", err)
	}
	c.InstanceID = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate without a number = %v, want nil", err)
	}
}

func TestALeadCampaignStillNeedsItsNumber(t *testing.T) {
	c := &Campaign{Name: "x", Message: MessageSpec{Kind: KindText, Bodies: []string{"Oi! Responda /sair para sair."}}, Targets: []TargetInput{{Number: "5511981124410"}}}
	c.Normalize()
	if err := c.ValidateMetadata(); !errors.Is(err, ErrCampaignInstanceIDRequired) {
		t.Fatalf("ValidateMetadata = %v, want ErrCampaignInstanceIDRequired", err)
	}
}

func TestAGroupCampaignSavedBeforeThePoolKeepsItsMetadataValid(t *testing.T) {
	c := groupCampaign(TargetInput{GroupJID: "120363153742561022@g.us"})
	c.InstanceID = "n1"
	c.Normalize()
	if err := c.ValidateMetadata(); err != nil {
		t.Fatalf("ValidateMetadata of an older group campaign = %v, want nil", err)
	}
}

func TestSenderPoolOfAGroupCampaign(t *testing.T) {
	c := groupCampaign()
	c.InstanceID = ""
	c.SenderInstanceIDs = []string{" n2 ", "n1", "n2", ""}
	c.Normalize()

	if !reflect.DeepEqual(c.SenderPool(), []string{"n1", "n2"}) {
		t.Fatalf("pool = %v, want the distinct trimmed numbers", c.SenderPool())
	}
	if !c.MaySendThrough("n1") || c.MaySendThrough("n3") || c.MaySendThrough("") {
		t.Fatalf("MaySendThrough does not follow the pool")
	}
}

func TestAnOlderGroupCampaignSendsThroughItsOwnNumber(t *testing.T) {
	c := groupCampaign()
	c.InstanceID = "n1"
	c.Normalize()
	if !reflect.DeepEqual(c.SenderPool(), []string{"n1"}) || !c.MaySendThrough("n1") || c.MaySendThrough("n2") {
		t.Fatalf("pool = %v, want only the number it was created with", c.SenderPool())
	}
}

func TestAGroupCampaignWithoutAPoolSendsThroughNobody(t *testing.T) {
	c := groupCampaign()
	c.InstanceID = ""
	c.Normalize()
	if len(c.SenderPool()) != 0 || c.MaySendThrough("n1") {
		t.Fatalf("an empty pool lets a number send")
	}
}

func TestAPersonCampaignSendsOnlyThroughItsNumber(t *testing.T) {
	c := &Campaign{InstanceID: "n1", SenderInstanceIDs: []string{"n2"}}
	c.Normalize()
	if !reflect.DeepEqual(c.SenderPool(), []string{"n1"}) || c.MaySendThrough("n2") {
		t.Fatalf("pool = %v, want only the campaign number", c.SenderPool())
	}
}

func TestAGroupCampaignRefusesANumberOnEdit(t *testing.T) {
	c := groupCampaign()
	c.InstanceID = ""
	c.Normalize()
	if err := c.RefuseNumberChange(&Campaign{InstanceID: "n2"}); !errors.Is(err, ErrGroupCampaignTakesNoNumber) {
		t.Fatalf("RefuseNumberChange = %v, want ErrGroupCampaignTakesNoNumber", err)
	}
	if err := c.RefuseNumberChange(&Campaign{}); err != nil {
		t.Fatalf("an edit without a number refused: %v", err)
	}
}

func TestEstimateOfOneNumberIsPendingTimesItsInterval(t *testing.T) {
	got := EstimateSendTime([][]string{{"n1"}, {"n1"}, {"n1"}}, map[string]time.Duration{"n1": 10 * time.Second})
	if got.Duration != 30*time.Second || got.Pending != 3 || got.Senders != 1 || got.Unsendable != 0 {
		t.Fatalf("estimate = %+v, want 30s over one number", got)
	}
}

func TestEstimateSpreadsEntriesAcrossTheNumbersThatSendInParallel(t *testing.T) {
	entries := [][]string{{"n1", "n2"}, {"n1", "n2"}, {"n1", "n2"}, {"n1", "n2"}, {"n1"}}
	got := EstimateSendTime(entries, map[string]time.Duration{"n1": 10 * time.Second, "n2": 10 * time.Second})
	if got.Duration != 30*time.Second || got.Senders != 2 || got.Pending != 5 {
		t.Fatalf("estimate = %+v, want 30s over two numbers in parallel", got)
	}
}

func TestEstimateLeavesOutEntriesNoNumberCanSend(t *testing.T) {
	got := EstimateSendTime([][]string{{"n1"}, {}, {"n9"}}, map[string]time.Duration{"n1": 8 * time.Second})
	if got.Duration != 8*time.Second || got.Pending != 1 || got.Unsendable != 2 {
		t.Fatalf("estimate = %+v, want one sendable entry and two left out", got)
	}
}

func TestEstimateOfNothingPendingIsZero(t *testing.T) {
	if got := EstimateSendTime(nil, nil); got.Duration != 0 || got.Pending != 0 || got.Senders != 0 {
		t.Fatalf("estimate = %+v, want zero", got)
	}
}

func TestPacingIntervalIsTheMiddleOfTheRange(t *testing.T) {
	if got := PacingInterval(3000, 12000); got != 7500*time.Millisecond {
		t.Fatalf("interval = %v, want 7.5s", got)
	}
}
