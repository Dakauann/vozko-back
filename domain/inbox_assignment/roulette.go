package inbox_assignment

import (
	"sort"
	"time"
)

// Candidate is one member the roulette may hand a conversation to.
//
// LastSeen zero means "no presence record at all" — never online, as far as the
// product can tell — which is distinct from "online a long time ago" and is
// dropped by BuildLastSeenRing for that reason.
type Candidate struct {
	UserID   string
	LastSeen time.Time
	Online   bool
}

// ResumePolicy is what to do when the previously-assigned user is no longer in
// the ring (left the workspace, lost the permission, aged out of the window).
//
// The two rings answer this differently, and that is deliberate:
//
//   - ResumeAtInsertionPoint — the online ring is sorted by user id, so the
//     insertion point of the departed user is "the next agent alphabetically".
//     That is where the historical sort.SearchStrings landed, and it is
//     preserved verbatim so switching this code path cannot change who gets a
//     conversation in the default mode.
//
//   - ResumeAtHead — the last-seen ring is sorted by recency, where an
//     insertion point by id is meaningless. The head is the most recently
//     online member, which is exactly the priority the mode exists to express.
type ResumePolicy int

const (
	ResumeAtInsertionPoint ResumePolicy = iota
	ResumeAtHead
)

// BuildOnlineRing orders the connected pool the way the roulette always has:
// by user id, so the ring is identical on every replica and the insertion-point
// resume in NextIndex is well defined.
//
// It sorts in place and returns the same slice, matching the previous inline
// sort.Strings call.
func BuildOnlineRing(userIDs []string) []string {
	sort.Strings(userIDs)
	return userIDs
}

// BuildLastSeenRing keeps the candidates seen within the window and orders them
// most-recently-online first.
//
// Ties break on user id ascending. That is not cosmetic: two agents who
// disconnected in the same second must produce the same ring on every replica
// and on every tick, or the round-robin pointer walks a slice whose order keeps
// changing and distribution degrades into a random walk.
//
// A candidate that is connected right now is expected to arrive with
// LastSeen == now (the resolver applies that overlay), so it sorts to the head
// naturally instead of through a special case here.
//
// Boundary: a candidate whose age is exactly window is KEPT. Only strictly
// older than the window is dropped.
func BuildLastSeenRing(cands []Candidate, now time.Time, window time.Duration) []string {
	kept := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.UserID == "" {
			continue
		}
		if !c.Online {
			if c.LastSeen.IsZero() {
				continue
			}
			if now.Sub(c.LastSeen) > window {
				continue
			}
		}
		kept = append(kept, c)
	}

	sort.Slice(kept, func(i, j int) bool {
		if !kept[i].LastSeen.Equal(kept[j].LastSeen) {
			return kept[i].LastSeen.After(kept[j].LastSeen)
		}
		return kept[i].UserID < kept[j].UserID
	})

	ring := make([]string, 0, len(kept))
	for _, c := range kept {
		ring = append(ring, c.UserID)
	}
	return ring
}

// NextIndex returns the position in the ring that follows lastAssigned.
//
// An empty lastAssigned, or a ring of one, starts at the head. When
// lastAssigned is absent from the ring the resume policy decides where to
// pick up; see ResumePolicy for why the two rings differ.
func NextIndex(ring []string, lastAssigned string, resume ResumePolicy) int {
	if len(ring) == 0 {
		return 0
	}
	if lastAssigned == "" {
		return 0
	}
	for i, u := range ring {
		if u == lastAssigned {
			return (i + 1) % len(ring)
		}
	}
	if resume == ResumeAtInsertionPoint {
		return sort.SearchStrings(ring, lastAssigned) % len(ring)
	}
	return 0
}

// NextAfter returns the ring member that follows userID, wrapping around.
//
// Used by the rescue sweep, which walks the ring for one stalled conversation
// without touching the workspace round-robin pointer.
//
// Returns "" only when there is genuinely nobody else to hand it to: an empty
// ring, or a ring whose single member is userID. When userID is absent from the
// ring — they left the workspace, lost the permission, or aged out of the
// window — the head is returned, which is the most recently online member and
// the same resume rule ResumeAtHead applies. Stranding a conversation on an
// owner who is no longer eligible is the one outcome that helps nobody.
func NextAfter(ring []string, userID string) string {
	if len(ring) == 0 || userID == "" {
		return ""
	}
	for i, u := range ring {
		if u == userID {
			if len(ring) == 1 {
				return ""
			}
			return ring[(i+1)%len(ring)]
		}
	}
	return ring[0]
}
