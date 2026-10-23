package carriermessaging

import (
	"slices"
	"testing"

	"vozko/domain/shared"
)

func TestChannels(t *testing.T) {
	for _, c := range AllChannels() {
		if !c.Valid() || string(c.EntryType()) != string(c) || c.InboundCharge().Channel() != c {
			t.Fatalf("channel %s is not fully described", c)
		}
	}
	if Channel("fax").Valid() {
		t.Fatal("unknown channel accepted")
	}
}

func TestChannelsMapToTheSharedEntryTypes(t *testing.T) {
	cases := []struct {
		channel Channel
		want    shared.EntryType
	}{
		{ChannelRCS, shared.EntryTypeRCS},
		{ChannelSMS, shared.EntryTypeSMS},
		{Channel("fax"), ""},
	}
	for _, tc := range cases {
		if got := tc.channel.EntryType(); got != tc.want {
			t.Fatalf("%q maps to %q, want %q", tc.channel, got, tc.want)
		}
	}
}

func TestRCSAndSMSWaitForTheCMComContract(t *testing.T) {
	for _, c := range AllChannels() {
		if c.Launched() {
			t.Fatalf("%s is launched before the CM.com contract and prices", c)
		}
	}
	if Channel("fax").Launched() {
		t.Fatal("an unknown channel is never launched")
	}
}

func TestLaunchedChannelsKeepTheCatalogOrder(t *testing.T) {
	var want []Channel
	for _, c := range AllChannels() {
		if c.Launched() {
			want = append(want, c)
		}
	}
	if got := LaunchedChannels(); !slices.Equal(got, want) {
		t.Fatalf("launched = %v, want %v", got, want)
	}
}
