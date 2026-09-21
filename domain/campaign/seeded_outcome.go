package campaign

import (
	"errors"
	"fmt"
	"math/rand"
)

var ErrSeededOutcomeOverflow = errors.New("seeded campaign outcome: sent and failed percentages cannot exceed 100 together")

type SeededOutcome struct {
	SentPercent   int `json:"sentPercent"`
	FailedPercent int `json:"failedPercent"`
}

func (o *SeededOutcome) Normalize() {
	if o == nil {
		return
	}
	o.SentPercent = clampPercent(o.SentPercent)
	o.FailedPercent = clampPercent(o.FailedPercent)
}

func (o *SeededOutcome) Validate() error {
	if o == nil {
		return nil
	}
	if o.SentPercent+o.FailedPercent > 100 {
		return fmt.Errorf("%w: %d + %d", ErrSeededOutcomeOverflow,
			o.SentPercent, o.FailedPercent)
	}
	return nil
}

func (o *SeededOutcome) Empty() bool {
	return o == nil || (o.SentPercent <= 0 && o.FailedPercent <= 0)
}

func (o *SeededOutcome) Statuses(total int) []SendStatus {
	if o.Empty() || total <= 0 {
		return nil
	}

	sentShare := clampPercent(o.SentPercent)
	failedShare := clampPercent(o.FailedPercent)

	sent := total * sentShare / 100
	settled := total * clampPercent(sentShare+failedShare) / 100
	failed := settled - sent

	out := make([]SendStatus, total)
	for i := range out {
		switch {
		case i < sent:
			out[i] = SendStatusSent
		case i < sent+failed:
			out[i] = SendStatusFailed
		default:
			out[i] = SendStatusPending
		}
	}

	seed := int64(total)*10_000 + int64(o.SentPercent)*100 + int64(o.FailedPercent)
	shuffle := rand.New(rand.NewSource(seed))
	shuffle.Shuffle(total, func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func clampPercent(v int) int {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
