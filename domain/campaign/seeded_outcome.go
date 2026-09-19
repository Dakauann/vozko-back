package campaign

import (
	"errors"
	"fmt"
	"math/rand"
)

// A campaign that is created already carrying results.
//
// This is a DEMONSTRATION control, not a product feature: it exists so a
// platform administrator can show a campaign that looks like one that ran,
// without waiting for a real blast to produce numbers. Nothing here sends
// anything, spends anything or touches a provider — it only decides which
// status each entry is born in, which is why it lives in the shared kernel
// beside the status vocabulary it hands out rather than in a channel package.
//
// "Responded" lands on READ because READ is the furthest state this vocabulary
// has: somebody who answered necessarily opened the message, and inventing an
// eighth status to say so would put a bucket in every export, filter and tile
// that no real send could ever produce.

// ErrSeededOutcomeOverflow means the two shares together claim more entries
// than the campaign has.
var ErrSeededOutcomeOverflow = errors.New("seeded campaign outcome: responded and failed percentages cannot exceed 100 together")

// SeededOutcome is the share of a campaign's entries to pre-settle at creation.
//
// Whatever the two shares leave over stays PENDING, which is what an entry
// nobody has sent to looks like anyway — so a mix of 30/10 reads as a campaign
// that is 40% of the way through, not as one that lost 60% of its list.
type SeededOutcome struct {
	RespondedPercent int `json:"respondedPercent"`
	FailedPercent    int `json:"failedPercent"`
}

// Normalize clamps both shares into range.
//
// Clamped rather than refused, the same bargain SeedScript.MaxMessages makes:
// an out-of-range percentage is a caller bug whose nearest legal value is
// obvious. Validate still refuses the one case with no obvious answer — two
// shares that together claim more entries than exist.
func (o *SeededOutcome) Normalize() {
	if o == nil {
		return
	}
	o.RespondedPercent = clampPercent(o.RespondedPercent)
	o.FailedPercent = clampPercent(o.FailedPercent)
}

// Validate reports whether this mix can be acted on at all. Nil is valid: it is
// the ordinary campaign, born entirely PENDING.
func (o *SeededOutcome) Validate() error {
	if o == nil {
		return nil
	}
	if o.RespondedPercent+o.FailedPercent > 100 {
		return fmt.Errorf("%w: %d + %d", ErrSeededOutcomeOverflow,
			o.RespondedPercent, o.FailedPercent)
	}
	return nil
}

// Empty reports whether this mix would settle nothing, so a caller can treat a
// zeroed struct exactly as it treats a nil one.
func (o *SeededOutcome) Empty() bool {
	return o == nil || (o.RespondedPercent <= 0 && o.FailedPercent <= 0)
}

// Statuses hands out one status per entry, in entry order.
//
// A nil slice means "settle nothing", so the caller keeps its own default
// rather than learning this type's. The two settled buckets are SPREAD through
// the list rather than bunched at its head: the entries table lists rows in
// creation order, and three solid blocks read as a broken import rather than as
// a campaign that ran. The spread is seeded from the request itself, so the
// same request always produces the same list and a test can pin it.
func (o *SeededOutcome) Statuses(total int) []SendStatus {
	if o.Empty() || total <= 0 {
		return nil
	}

	// Cumulative flooring, NOT one floor per bucket.
	//
	// Two independent floors can leave up to two entries over: 40% and 60% of
	// three targets floors to one and one, and the third stays PENDING even
	// though the operator asked for the whole list. That reads as a bug, and it
	// is worse than it reads — a stray PENDING row is a live target that a
	// later Start would really send to.
	//
	// Taking each bucket as the difference between cumulative floors puts the
	// rounding in ONE place. Shares that add up to 100 then settle the list
	// exactly, and every bucket stays within one entry of its true share.
	respondedShare := clampPercent(o.RespondedPercent)
	failedShare := clampPercent(o.FailedPercent)

	responded := total * respondedShare / 100
	// The cumulative share is clamped too, so a caller that skipped Validate and
	// asked for more than the whole list gets the whole list, never more.
	settled := total * clampPercent(respondedShare+failedShare) / 100
	failed := settled - responded

	out := make([]SendStatus, total)
	for i := range out {
		switch {
		case i < responded:
			out[i] = SendStatusRead
		case i < responded+failed:
			out[i] = SendStatusFailed
		default:
			out[i] = SendStatusPending
		}
	}

	seed := int64(total)*10_000 + int64(o.RespondedPercent)*100 + int64(o.FailedPercent)
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
