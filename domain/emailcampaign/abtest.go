package emailcampaign

import (
	"errors"
	"hash/fnv"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emaildesign"
)

const (
	MinTestPercent        = 10
	MaxTestPercent        = 50
	MinDecideAfterMinutes = 30
	MaxDecideAfterMinutes = 1440
	assignmentBuckets     = 10000
)

var (
	ErrABTestPercent      = errors.New("emailcampaign: the test share must be between 10 and 50 percent")
	ErrABTestMetric       = errors.New("emailcampaign: the winner is chosen by opens or clicks")
	ErrABTestWindow       = errors.New("emailcampaign: the test runs between 30 minutes and 24 hours")
	ErrABTestSubject      = errors.New("emailcampaign: the subject of variant b is invalid")
	ErrABTestWinnerPreset = errors.New("emailcampaign: the winner is decided by the results")
)

type Variant string

const (
	VariantA      Variant = "a"
	VariantB      Variant = "b"
	VariantWinner Variant = "winner"
)

type WinnerMetric string

const (
	WinnerOpens  WinnerMetric = "opens"
	WinnerClicks WinnerMetric = "clicks"
)

type ABTest struct {
	VariantBSubject    string       `json:"variant_b_subject"`
	TestPercent        int          `json:"test_percent"`
	WinnerMetric       WinnerMetric `json:"winner_metric"`
	DecideAfterMinutes int          `json:"decide_after_minutes"`
	Winner             Variant      `json:"winner,omitempty"`
}

func (a ABTest) Validate() error {
	switch {
	case a.TestPercent < MinTestPercent || a.TestPercent > MaxTestPercent:
		return ErrABTestPercent
	case a.WinnerMetric != WinnerOpens && a.WinnerMetric != WinnerClicks:
		return ErrABTestMetric
	case a.DecideAfterMinutes < MinDecideAfterMinutes || a.DecideAfterMinutes > MaxDecideAfterMinutes:
		return ErrABTestWindow
	case len(emaildesign.ValidateSubject(a.VariantBSubject)) > 0:
		return ErrABTestSubject
	case a.Winner != "":
		return ErrABTestWinnerPreset
	}
	return nil
}

func (a ABTest) Assign(address emailaddress.Address) Variant {
	h := fnv.New64a()
	h.Write([]byte("emailcampaign.ab|"))
	h.Write([]byte(address))
	bucket := int(h.Sum64() % assignmentBuckets)
	share := a.TestPercent * assignmentBuckets / 100
	switch {
	case bucket < share/2:
		return VariantA
	case bucket < share:
		return VariantB
	}
	return VariantWinner
}

func (a ABTest) DecideAt(startedAt time.Time) time.Time {
	return startedAt.Add(time.Duration(a.DecideAfterMinutes) * time.Minute)
}

type VariantResult struct {
	Delivered int64 `json:"delivered"`
	Opens     int64 `json:"opens"`
	Clicks    int64 `json:"clicks"`
}

func (r VariantResult) hits(metric WinnerMetric) int64 {
	if metric == WinnerClicks {
		return r.Clicks
	}
	return r.Opens
}

func ChooseWinner(metric WinnerMetric, a, b VariantResult) Variant {
	if b.Delivered == 0 {
		return VariantA
	}
	if a.Delivered == 0 {
		if b.hits(metric) > 0 {
			return VariantB
		}
		return VariantA
	}
	if b.hits(metric)*a.Delivered > a.hits(metric)*b.Delivered {
		return VariantB
	}
	return VariantA
}
