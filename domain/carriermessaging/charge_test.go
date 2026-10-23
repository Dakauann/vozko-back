package carriermessaging

import (
	"testing"

	"vozko/domain/rcs"
)

func TestChargeKindOfAClass(t *testing.T) {
	if ChargeForClass(rcs.ClassBasic) != ChargeRCSBasic || ChargeForClass(rcs.ClassSingle) != ChargeRCSSingle {
		t.Fatal("each class is charged by its own pricing item")
	}
}

func TestEveryChargeKindHasAPricingItem(t *testing.T) {
	want := map[ChargeKind][3]string{
		ChargeRCSBasic:        {"rcs", "basic", "per_message"},
		ChargeRCSSingle:       {"rcs", "single", "per_message"},
		ChargeRCSConversation: {"rcs", "conversation", "per_conversation"},
		ChargeRCSInbound:      {"rcs", "inbound", "per_message"},
		ChargeSMSSegment:      {"sms", "segment", "per_segment"},
		ChargeSMSInbound:      {"sms", "inbound", "per_message"},
	}
	if len(AllChargeKinds()) != len(want) {
		t.Fatalf("AllChargeKinds = %v", AllChargeKinds())
	}
	for _, k := range AllChargeKinds() {
		got := [3]string{string(k.Channel()), k.PricingService(), k.PricingMetric()}
		if got != want[k] {
			t.Fatalf("%s = %v, want %v", k, got, want[k])
		}
	}
	if ChargeKind("fax").PricingService() != "" || ChargeKind("fax").Channel().Valid() {
		t.Fatal("an unknown kind has no pricing item")
	}
}

func TestChargeKindsOfAChannel(t *testing.T) {
	sms := ChargeKindsOf(ChannelSMS)
	if len(sms) != 2 || sms[0] != ChargeSMSSegment || sms[1] != ChargeSMSInbound {
		t.Fatalf("sms kinds = %v", sms)
	}
}

func TestReplyFloor(t *testing.T) {
	cases := []struct {
		name              string
		spend30d, inbound int64
		want              int64
	}{
		{"small sender keeps a hundred replies", 20_000_000, 31_667, 3_166_700},
		{"big sender keeps a tenth of its month", 900_000_000, 31_667, 90_000_000},
		{"new sender keeps a hundred replies", 0, 31_667, 3_166_700},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReplyFloor(tc.spend30d, tc.inbound); got != tc.want {
				t.Fatalf("ReplyFloor = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSendKeepsTheReplyFloor(t *testing.T) {
	cases := []struct {
		balance, price, floor int64
		want                  bool
	}{
		{1_000_000, 100_000, 900_000, true},
		{1_000_000, 100_001, 900_000, false},
		{50_000, 100_000, 0, false},
		{0, 0, 0, false},
	}
	for _, tc := range cases {
		if got := AffordsSend(tc.balance, tc.price, tc.floor); got != tc.want {
			t.Fatalf("AffordsSend(%d, %d, %d) = %v, want %v", tc.balance, tc.price, tc.floor, got, tc.want)
		}
	}
}

func TestShortfallTellsHowMuchIsMissing(t *testing.T) {
	if got := Shortfall(1_000_000, 800_000, 300_000); got != 100_000 {
		t.Fatalf("Shortfall = %d", got)
	}
	if got := Shortfall(1_000_000, 500_000, 300_000); got != 0 {
		t.Fatalf("Shortfall = %d, want 0", got)
	}
}

func TestChargeReferencesAreStablePerMessageAndKind(t *testing.T) {
	if ChargeReference(ChargeRCSSingle, "abc") != "carrier:rcs_single:abc" {
		t.Fatalf("got %q", ChargeReference(ChargeRCSSingle, "abc"))
	}
	if ChargeReference(ChargeRCSConversation, "c") == ChargeReference(ChargeRCSInbound, "c") {
		t.Fatal("different kinds never share a reference")
	}
}
