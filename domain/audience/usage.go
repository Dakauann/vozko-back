package audience

import (
	"context"
	"fmt"
	"time"
)

// How much a workspace may analyse, over a window that MOVES.
//
// This replaces a counter keyed on the UTC calendar date, which had three
// problems and no compensating virtue:
//
//   - The reset was arbitrary and invisible. A workspace at UTC-3 got its whole
//     allowance back at 21:00 local, which corresponds to nothing an operator
//     recognises, and a burst an hour earlier could spend the day twice over.
//   - It was a cliff. Analysis stopped dead, the queue piled up, and at midnight
//     the backlog flooded through in one go: the cap's own reset produced the
//     spike the cap exists to prevent.
//   - Nothing was ever returned. A batch that was counted and then failed at the
//     provider stayed spent, so a bad afternoon upstream could exhaust the
//     budget having classified nothing.
//
// A rolling window has no reset to fall off, no timezone to be surprised by,
// and it recovers continuously: the oldest hour ages out while the newest fills.

const (
	// UsageWindow is how far back the budget looks. Twenty-four hours keeps the
	// operator's mental model ("a daily cap") while removing the calendar.
	UsageWindow = 24 * time.Hour

	// UsageBucket is the resolution usage is recorded at.
	//
	// An hour, deliberately. The window then ages out in 24 steps rather than
	// one, which is what removes the cliff, and the cost of reading it is a
	// single hash of at most 24 small fields. Finer buckets would buy precision
	// that a spend ceiling in the thousands has no use for.
	UsageBucket = time.Hour
)

// Usage is a workspace's consumption against its budget, as a dashboard would
// show it.
type Usage struct {
	// Used is how many analyses were claimed inside the window.
	Used int `json:"used"`
	// Limit is the ceiling in force. Zero means unlimited, which is what a
	// workspace with no configured cap and no default would get.
	Limit int `json:"limit"`
	// OldestAt is when the earliest still-counted analysis was claimed, so a
	// screen can say when room next frees up rather than naming a reset that
	// no longer exists. Zero when nothing is counted.
	OldestAt time.Time `json:"oldestAt,omitempty"`
	// Waiting is how much is queued and not yet classified.
	//
	// Without it the budget reads as a number with no consequence, and an
	// operator who sets the ceiling too low sees only that the dashboard has
	// stopped moving. Work is never thrown away when the budget runs out: the
	// rows stay pending and the next pass picks them up. What a low ceiling
	// actually costs is delay, so the delay is what gets shown.
	Waiting int `json:"waiting"`
}

// Constrained reports whether the CEILING, rather than a lack of work, is what
// is holding analysis back.
//
// True when more is waiting than the window can still absorb, which is the only
// state where raising the limit would change anything. An exhausted budget with
// an empty queue is deliberately not constrained: everything that wanted
// analysing got it, and warning about that would train the operator to ignore
// the warning for the time it means something.
func (u Usage) Constrained() bool {
	return u.Limit > 0 && u.Waiting > u.Remaining()
}

// ClearsIn estimates how long the backlog takes to drain at the current ceiling,
// zero when it drains on the next pass or when there is no ceiling to wait for.
//
// The overflow is what has to wait for the window to free room, and the window
// frees a whole Limit over a whole UsageWindow, so this is a straight
// proportion. An approximation on purpose: it ignores where inside the window
// the claims fell and it assumes nothing new arrives. It exists to answer "is my
// limit roughly right", which it does at that resolution, and not to promise a
// completion time.
func (u Usage) ClearsIn() time.Duration {
	if u.Limit <= 0 {
		return 0
	}
	overflow := u.Waiting - u.Remaining()
	if overflow <= 0 {
		return 0
	}
	return time.Duration(float64(overflow) / float64(u.Limit) * float64(UsageWindow))
}

// Remaining is how much more may be analysed right now. Never negative, and
// meaningless (reported as zero) without a limit.
func (u Usage) Remaining() int {
	if u.Limit <= 0 {
		return 0
	}
	if u.Used >= u.Limit {
		return 0
	}
	return u.Limit - u.Used
}

// Exhausted reports whether the budget is spent. A workspace with no limit is
// never exhausted.
func (u Usage) Exhausted() bool {
	return u.Limit > 0 && u.Used >= u.Limit
}

// FreesAt is when the oldest counted analysis leaves the window, which is the
// soonest the budget can grow. Zero when nothing is counted, because then there
// is nothing to wait for.
func (u Usage) FreesAt() time.Time {
	if u.OldestAt.IsZero() {
		return time.Time{}
	}
	return u.OldestAt.Add(UsageWindow)
}

// UsageBucketKey names the bucket an instant falls in.
//
// Pure and exported so the store and its tests agree on the shape by
// construction rather than by both spelling out the same format string.
func UsageBucketKey(at time.Time) string {
	return at.UTC().Truncate(UsageBucket).Format("2006-01-02T15")
}

// ParseUsageBucketKey is the inverse, for summing a stored window.
func ParseUsageBucketKey(key string) (time.Time, error) {
	at, err := time.Parse("2006-01-02T15", key)
	if err != nil {
		return time.Time{}, fmt.Errorf("audience: bad usage bucket %q: %w", key, err)
	}
	return at.UTC(), nil
}

// UsageBucketsInWindow lists the buckets that make up the window ending at now,
// newest first. Anything outside is expired and must not be counted.
func UsageBucketsInWindow(now time.Time) []string {
	count := int(UsageWindow / UsageBucket)
	keys := make([]string, 0, count)
	start := now.UTC().Truncate(UsageBucket)
	for i := 0; i < count; i++ {
		keys = append(keys, UsageBucketKey(start.Add(-time.Duration(i)*UsageBucket)))
	}
	return keys
}

// UsageLimiter bounds how much a workspace analyses over the rolling window.
//
// Three operations rather than one, because the old single "reserve" could not
// express the two things that actually happen: work that was claimed and never
// done has to be given back, and a dashboard needs to read the number without
// spending any of it.
type UsageLimiter interface {
	// Claim records items against the workspace's budget and reports whether
	// they fit. A false means the caller must not classify them.
	Claim(ctx context.Context, workspaceID string, items, limit int, now time.Time) (bool, error)

	// Release returns items that were claimed and then not classified, so a
	// provider outage costs no budget.
	Release(ctx context.Context, workspaceID string, items int, now time.Time) error

	// Read reports consumption without changing it.
	Read(ctx context.Context, workspaceID string, limit int, now time.Time) (Usage, error)
}

// WorkspaceSettings is what the WORKSPACE decides about its own analysis, as
// opposed to what each channel account decides.
//
// Both fields use zero for "never set", which is what every workspace reads as
// until an operator picks a number, and is why this needs no migration of
// anything: an empty row and an absent row mean the same thing.
type WorkspaceSettings struct {
	// DailyCap is the rolling volume ceiling.
	//
	// It had lived on Settings, which is keyed on (source, account), and that
	// was the wrong shape for it in two ways a dashboard made obvious. The
	// budget it governs is counted per WORKSPACE, so a workspace whose accounts
	// disagreed had no single ceiling to report. And a workspace that analyses
	// only conversations has no channel account to configure, so it could not
	// set its ceiling at all: it ran under a number it could neither see nor
	// change, and the first sign of reaching it was analysis quietly stopping.
	DailyCap int
	// DebounceMinutes is how long a conversation must stay quiet before it is
	// handed to the engine. See debounce_window.go for why this is a setting
	// and not a constant.
	DebounceMinutes int
}

// WorkspaceSettingsStore persists them.
//
// One port over one row rather than one per setting: two narrow stores writing
// the same row is two writes that can clobber each other, which is the bug this
// table was given its own existence to avoid in the first place.
type WorkspaceSettingsStore interface {
	Get(ctx context.Context, workspaceID string) (WorkspaceSettings, error)
	// Save writes the whole record, so callers read first and change what they
	// mean to. Explicit, because the alternative is a partial-update API whose
	// zero values are indistinguishable from "leave this alone".
	Save(ctx context.Context, workspaceID string, settings WorkspaceSettings) error
}

// ResolveDailyCap is the ONE rule for which ceiling is in force.
//
// Both the engine, which stops a pass, and the dashboard, which reports the
// number, go through this. They used to answer it separately, the engine from
// the resolved account settings and the dashboard from the highest row it could
// find, which is exactly how a screen ends up confidently naming a limit that is
// not the one being enforced.
//
// Precedence is workspace, then account, then the product default. The account
// step is what keeps this additive: every workspace configured before the
// workspace-level control existed carries on under the cap it already had.
func ResolveDailyCap(workspaceCap, accountCap int) int {
	if workspaceCap > 0 {
		return workspaceCap
	}
	if accountCap > 0 {
		return accountCap
	}
	return DefaultDailyCap
}
