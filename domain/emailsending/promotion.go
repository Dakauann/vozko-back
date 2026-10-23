package emailsending

import "time"

const (
	PromotionCleanDays          = 14
	PromotionMinDelivered       = 200
	PromotionMaxBouncePerMil    = 20
	PromotionMaxComplaintPer10k = 8
)

type PromotionTargets struct {
	Days               int
	MinDelivered       int64
	MaxBouncePerMil    int64
	MaxComplaintPer10k int64
}

func CurrentPromotionTargets() PromotionTargets {
	return PromotionTargets{Days: PromotionCleanDays, MinDelivered: PromotionMinDelivered, MaxBouncePerMil: PromotionMaxBouncePerMil, MaxComplaintPer10k: PromotionMaxComplaintPer10k}
}

type HealthWindow struct {
	Delivered         int64
	HardBounces       int64
	Complaints        int64
	OpenHighFindings  int
	FailedTestBatches int
}

type PromotionOutcome string

const (
	PromotionGranted     PromotionOutcome = "granted"
	PromotionNotEligible PromotionOutcome = "not_eligible"
	PromotionTooSoon     PromotionOutcome = "too_soon"
	PromotionLowVolume   PromotionOutcome = "low_volume"
	PromotionBounces     PromotionOutcome = "bounces"
	PromotionComplaints  PromotionOutcome = "complaints"
	PromotionFindings    PromotionOutcome = "findings"
	PromotionTestBatch   PromotionOutcome = "test_batch"
)

func Promotion(account SenderAccount, window HealthWindow, now time.Time) PromotionOutcome {
	if account.Mode != ModeProbation || account.Status != AccountActive || account.ProbationStartedAt.IsZero() {
		return PromotionNotEligible
	}
	if daysBetween(account.ProbationStartedAt, now) < PromotionCleanDays {
		return PromotionTooSoon
	}
	if window.OpenHighFindings > 0 {
		return PromotionFindings
	}
	if window.FailedTestBatches > 0 {
		return PromotionTestBatch
	}
	if window.Delivered < PromotionMinDelivered {
		return PromotionLowVolume
	}
	if window.HardBounces*1000 >= PromotionMaxBouncePerMil*window.Delivered {
		return PromotionBounces
	}
	if window.Complaints*10000 >= PromotionMaxComplaintPer10k*window.Delivered {
		return PromotionComplaints
	}
	return PromotionGranted
}

func PromotionWindowStart(now time.Time) time.Time {
	return now.AddDate(0, 0, -PromotionCleanDays)
}

func daysBetween(from, to time.Time) int {
	f := from.In(brasilia)
	t := to.In(brasilia)
	start := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, brasilia)
	end := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, brasilia)
	return int(end.Sub(start).Hours() / 24)
}
