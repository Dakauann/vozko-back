package emailsending

import (
	"testing"
	"time"
)

func TestPromotionVerdict(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	started := func(days int) time.Time { return now.AddDate(0, 0, -days) }
	healthy := HealthWindow{Delivered: 1000, HardBounces: 10, Complaints: 0}
	cases := []struct {
		name    string
		account SenderAccount
		window  HealthWindow
		want    PromotionOutcome
	}{
		{name: "fourteen clean days promote", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(14)}, window: healthy, want: PromotionGranted},
		{name: "thirteen days is too soon", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(13)}, window: healthy, want: PromotionTooSoon},
		{name: "too little volume", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(20)}, window: HealthWindow{Delivered: 199}, want: PromotionLowVolume},
		{name: "bounce rate at two percent fails", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(20)}, window: HealthWindow{Delivered: 1000, HardBounces: 20}, want: PromotionBounces},
		{name: "complaint rate at eight per ten thousand fails", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(20)}, window: HealthWindow{Delivered: 10000, Complaints: 8}, want: PromotionComplaints},
		{name: "complaint rate just under the line passes", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(20)}, window: HealthWindow{Delivered: 10000, Complaints: 7}, want: PromotionGranted},
		{name: "an open high finding blocks", account: SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: started(20)}, window: HealthWindow{Delivered: 1000, OpenHighFindings: 1}, want: PromotionFindings},
		{name: "a paused account is never promoted", account: SenderAccount{Mode: ModeProbation, Status: AccountPausedByProvider, ProbationStartedAt: started(20)}, window: healthy, want: PromotionNotEligible},
		{name: "a trusted account is not promoted again", account: SenderAccount{Mode: ModeTrusted, Status: AccountActive, ProbationStartedAt: started(40)}, window: healthy, want: PromotionNotEligible},
		{name: "a missing probation start is not eligible", account: SenderAccount{Mode: ModeProbation, Status: AccountActive}, window: healthy, want: PromotionNotEligible},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Promotion(tc.account, tc.window, now); got != tc.want {
				t.Fatalf("Promotion = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromotionTargetsMirrorTheRule(t *testing.T) {
	got := CurrentPromotionTargets()
	want := PromotionTargets{Days: PromotionCleanDays, MinDelivered: PromotionMinDelivered, MaxBouncePerMil: PromotionMaxBouncePerMil, MaxComplaintPer10k: PromotionMaxComplaintPer10k}
	if got != want {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
}

func TestAFailedTestBatchBlocksPromotion(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	account := SenderAccount{Mode: ModeProbation, Status: AccountActive, ProbationStartedAt: now.AddDate(0, 0, -20)}
	window := HealthWindow{Delivered: 1000, FailedTestBatches: 1}
	if got := Promotion(account, window, now); got != PromotionTestBatch {
		t.Fatalf("Promotion = %q, want %q", got, PromotionTestBatch)
	}
}
