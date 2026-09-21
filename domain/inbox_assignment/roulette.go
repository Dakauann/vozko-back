package inbox_assignment

import (
	"sort"
	"time"
)

type Candidate struct {
	UserID   string
	LastSeen time.Time
	Online   bool
}

type ResumePolicy int

const (
	ResumeAtInsertionPoint ResumePolicy = iota
	ResumeAtHead
)

func BuildOnlineRing(userIDs []string) []string {
	sort.Strings(userIDs)
	return userIDs
}

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
