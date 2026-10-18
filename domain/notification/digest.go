package notification

import "time"

const (
	DigestHour      = 8
	DigestItemLimit = 20
)

type Digest struct {
	WorkspaceID string
	UserID      string
	Items       []Item
	Total       int
}

func (d Digest) More() int {
	if more := d.Total - len(d.Items); more > 0 {
		return more
	}
	return 0
}

func (d Digest) Empty() bool {
	return d.Total == 0
}

func (p Preferences) HoldsForDigest(severity Severity) bool {
	return p.EmailDigest && severity != SeverityCritical
}

func DigestCutoff(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), DigestHour, 0, 0, 0, loc)
	if local.Before(cutoff) {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	return cutoff
}
