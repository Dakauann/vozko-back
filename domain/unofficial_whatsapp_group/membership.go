package unofficial_whatsapp_group

import (
	"sort"
	"strings"

	"vozko/domain/shared"
)

func BuildMembershipPage(rows []MembershipStat, by MembershipSort, desc bool, p shared.Pagination) MembershipPage {
	stats := make([]*MembershipStat, 0, len(rows))
	var summary MembershipSummary
	for i := range rows {
		s := rows[i]
		s.Net = s.Joins - s.Leaves
		s.ChurnRate = ChurnRate(s.Leaves, s.Base)
		s.GrowthRate = GrowthRate(s.Net, s.Base)
		summary.StartMembers += s.Base
		summary.Members += s.Members
		summary.Joins += s.Joins
		summary.Leaves += s.Leaves
		summary.Messages += s.Messages
		stats = append(stats, &s)
	}
	summary.Net = summary.Joins - summary.Leaves
	summary.Groups = len(stats)
	summary.ChurnRate = ChurnRate(summary.Leaves, summary.StartMembers)
	summary.GrowthRate = GrowthRate(summary.Net, summary.StartMembers)

	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		if rate := rateOf(a, by); rate != nil || rateOf(b, by) != nil {
			if (rate == nil) != (rateOf(b, by) == nil) {
				return rate != nil
			}
		}
		less, equal := compareMembership(a, b, by)
		if equal {
			return strings.ToLower(a.Subject) < strings.ToLower(b.Subject)
		}
		if desc {
			return !less
		}
		return less
	})

	norm := shared.NormalizePagination(p)
	start := norm.Offset()
	if start > len(stats) {
		start = len(stats)
	}
	end := start + norm.PageSize
	if end > len(stats) {
		end = len(stats)
	}
	return MembershipPage{
		Page:    shared.NewPaginatedResult(stats[start:end], norm, int64(len(stats))),
		Summary: summary,
	}
}

func rateOf(s *MembershipStat, by MembershipSort) *float64 {
	switch by {
	case MembershipByChurn:
		return s.ChurnRate
	case MembershipByGrowth:
		return s.GrowthRate
	}
	return nil
}

func compareMembership(a, b *MembershipStat, by MembershipSort) (less, equal bool) {
	cmp := func(x, y int64) (bool, bool) { return x < y, x == y }
	switch by {
	case MembershipByMembers:
		return cmp(a.Members, b.Members)
	case MembershipByJoins:
		return cmp(a.Joins, b.Joins)
	case MembershipByLeaves:
		return cmp(a.Leaves, b.Leaves)
	case MembershipByNet:
		return cmp(a.Net, b.Net)
	case MembershipByMessages:
		return cmp(a.Messages, b.Messages)
	case MembershipByChurn, MembershipByGrowth:
		x, y := rateOf(a, by), rateOf(b, by)
		if x == nil || y == nil {
			return false, true
		}
		return *x < *y, *x == *y
	}
	x, y := strings.ToLower(a.Subject), strings.ToLower(b.Subject)
	return x < y, x == y
}
