package emailsending

import (
	"context"
	"testing"
)

func TestProviderLaneTravelsInTheContextAndDefaultsToTheAccountLane(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want ProviderLane
	}{
		{name: "nothing set", ctx: context.Background(), want: LaneAccount},
		{name: "protect", ctx: WithProviderLane(context.Background(), LaneProtect), want: LaneProtect},
		{name: "background", ctx: WithProviderLane(context.Background(), LaneBackground), want: LaneBackground},
		{name: "the innermost lane wins", ctx: WithProviderLane(WithProviderLane(context.Background(), LaneBackground), LaneProtect), want: LaneProtect},
		{name: "an unknown lane falls back to the account lane", ctx: WithProviderLane(context.Background(), ProviderLane(9)), want: LaneAccount},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProviderLaneOf(tc.ctx); got != tc.want {
				t.Fatalf("lane = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestALowerLaneYieldsWhileAHigherLaneWaits(t *testing.T) {
	cases := []struct {
		name    string
		lane    ProviderLane
		waiting []ProviderLane
		want    bool
	}{
		{name: "alone", lane: LaneBackground, want: true},
		{name: "background behind protect", lane: LaneBackground, waiting: []ProviderLane{LaneProtect}, want: false},
		{name: "background behind account", lane: LaneBackground, waiting: []ProviderLane{LaneAccount}, want: false},
		{name: "account behind protect", lane: LaneAccount, waiting: []ProviderLane{LaneProtect}, want: false},
		{name: "account ahead of background", lane: LaneAccount, waiting: []ProviderLane{LaneBackground}, want: true},
		{name: "protect never yields", lane: LaneProtect, waiting: []ProviderLane{LaneAccount, LaneBackground}, want: true},
		{name: "the same lane does not yield to itself", lane: LaneAccount, waiting: []ProviderLane{LaneAccount}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			waiting := func(l ProviderLane) bool {
				for _, w := range tc.waiting {
					if w == l {
						return true
					}
				}
				return false
			}
			if got := LaneMayCall(tc.lane, waiting); got != tc.want {
				t.Fatalf("LaneMayCall(%s) = %v, want %v", tc.lane, got, tc.want)
			}
		})
	}
}

func TestOnlyTheSendingActionsSkipTheManagementCallRate(t *testing.T) {
	cases := []struct {
		operation string
		want      bool
	}{
		{operation: "SendEmail", want: false},
		{operation: "SendRawEmail", want: false},
		{operation: "SendTemplatedEmail", want: false},
		{operation: "GetAccount", want: true},
		{operation: "UpdateReceiptRule", want: true},
		{operation: "PutSuppressedDestination", want: true},
		{operation: "", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			if got := SESManagementCall(tc.operation); got != tc.want {
				t.Fatalf("SESManagementCall(%q) = %v, want %v", tc.operation, got, tc.want)
			}
		})
	}
}
