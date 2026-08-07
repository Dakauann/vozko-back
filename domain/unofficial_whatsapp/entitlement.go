package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
)

// How many numbers a workspace may connect.
//
// The allowance is granted per workspace by a platform administrator and topped
// up by purchasable addons — never implied by whichever plan the workspace is
// on. Three facts make that the right shape rather than a plan field:
//
//  1. Every connected number occupies a slot on a host WE operate, with a hard
//     ceiling the provider answers 429 past. Capacity here costs us money and is
//     finite in a way seats are not.
//  2. The channel is unofficial. A number that gets banned is a customer losing
//     their WhatsApp, and that is a risk somebody should accept deliberately for
//     a specific workspace rather than inherit from a pricing tier.
//  3. There is no per-plan price for it yet. Encoding one in a plan field would
//     be inventing a product decision that has not been made.
//
// The RULE lives here, in the domain, because two callers need the same answer:
// the provisioning use case, which must refuse, and the read path that shows an
// operator "3 of 5 used" before they click. A rule duplicated in both would
// eventually disagree, and the disagreement would look like the UI lying.

var (
	// ErrNoInstanceAllowance means the workspace has been granted nothing. It is
	// worth its own error because the remedy is completely different from being
	// full: talk to us, rather than buy one more.
	ErrNoInstanceAllowance = errors.New("this workspace has no unofficial whatsapp numbers included")
	// ErrInstanceLimitReached means every granted slot is in use.
	ErrInstanceLimitReached = errors.New("all unofficial whatsapp numbers included in this workspace are in use")
)

// InstanceAllowance is what a workspace may do right now, and why.
//
// Returned as a value rather than a bare bool so one read answers both questions
// the UI asks — may I connect another, and what should the counter say — without
// the screen re-deriving the second from the first.
type InstanceAllowance struct {
	// Limit is the effective entitlement: the granted allowance plus active
	// addons.
	Limit int
	// Used is how many slots the workspace currently holds. Counts every
	// non-deleted instance, connected or not: the slot is held on the host
	// either way.
	Used int

	// Granted and Purchased are the two halves Limit is made of, carried
	// separately so the UI can say "2 included + 3 purchased" rather than only
	// "5". The distinction is the whole product model: one half is a decision we
	// made about this workspace, the other is something they bought — and an
	// operator looking at a full meter needs to know which lever to pull.
	Granted   int
	Purchased int
}

// Remaining is how many more numbers may be connected. Never negative: a
// workspace whose allowance was reduced below what it already holds is over, not
// owed slots, and reporting -2 would render as nonsense.
func (a InstanceAllowance) Remaining() int {
	if a.Used >= a.Limit {
		return 0
	}
	return a.Limit - a.Used
}

// CanProvision reports whether one more number may be connected.
func (a InstanceAllowance) CanProvision() bool { return a.Used < a.Limit }

// OverLimit reports whether the workspace holds MORE than it is entitled to.
//
// Reachable without anyone doing anything wrong: an addon lapses, or an
// administrator lowers a grant. Deliberately does NOT imply any automatic
// action — see Enforce.
func (a InstanceAllowance) OverLimit() bool { return a.Used > a.Limit }

// Enforce returns the error to refuse a provisioning attempt with, or nil.
//
// It distinguishes "you have none" from "you have none LEFT" because the
// operator's next step differs: the first needs an allowance granted, the second
// needs an addon bought. A single "not allowed" would send both to support.
func (a InstanceAllowance) Enforce() error {
	if a.Limit <= 0 {
		return ErrNoInstanceAllowance
	}
	if !a.CanProvision() {
		return fmt.Errorf("%w: %d of %d in use", ErrInstanceLimitReached, a.Used, a.Limit)
	}
	return nil
}

// InstanceEntitlementReader resolves a workspace's allowance.
//
// A port rather than a direct dependency on the addon package: this channel must
// not know how an entitlement is composed — a plan base, a config grant, active
// addons — only what the total is. That is also what lets the composition change
// (it already differs from every other kind) without touching this channel.
type InstanceEntitlementReader interface {
	// AllowanceFor reports the workspace's limit and current usage.
	AllowanceFor(ctx context.Context, workspaceID string) (InstanceAllowance, error)
}

// ErrEntitlementUnavailable means the allowance could not be resolved at all.
//
// Provisioning FAILS CLOSED on it. Treating an unreadable entitlement as
// unlimited would let a billing outage hand out capacity on hosts we pay for,
// and every slot handed out that way is one a paying workspace cannot use.
var ErrEntitlementUnavailable = errors.New("unofficial whatsapp: could not resolve this workspace's number allowance")
