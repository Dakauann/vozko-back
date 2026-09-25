package attendance

import (
	"sort"

	"vozko/domain/actor"
)

type RevenueSource string

const (
	RevenueSourceHuman    RevenueSource = "human"
	RevenueSourceAI       RevenueSource = "ai"
	RevenueSourceWorkflow RevenueSource = "workflow"
	RevenueSourceUnowned  RevenueSource = "unowned"
)

type RevenueSourceRow struct {
	Source     RevenueSource `json:"source"`
	Currency   string        `json:"currency"`
	WonCount   int64         `json:"won_count"`
	ValueCents int64         `json:"value_cents"`
}

func revenueSourceOf(ownerID string) RevenueSource {
	if ownerID == "" {
		return RevenueSourceUnowned
	}
	switch actor.KindOf(ownerID) {
	case actor.KindAI:
		return RevenueSourceAI
	case actor.KindWorkflow:
		return RevenueSourceWorkflow
	case actor.KindHuman:
		return RevenueSourceHuman
	}
	return RevenueSourceUnowned
}

var revenueSourceOrder = map[RevenueSource]int{
	RevenueSourceHuman:    0,
	RevenueSourceAI:       1,
	RevenueSourceWorkflow: 2,
	RevenueSourceUnowned:  3,
}

func revenueBySource(tallies []RevenueTally) []RevenueSourceRow {
	type key struct {
		source   RevenueSource
		currency string
	}
	rows := map[key]*RevenueSourceRow{}
	for _, t := range tallies {
		if t.Currency == "" {
			continue
		}
		k := key{source: revenueSourceOf(t.OwnerID), currency: t.Currency}
		row, found := rows[k]
		if !found {
			row = &RevenueSourceRow{Source: k.source, Currency: k.currency}
			rows[k] = row
		}
		row.WonCount += t.WonCount
		row.ValueCents += t.ValueCents
	}
	out := make([]RevenueSourceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return revenueSourceOrder[out[i].Source] < revenueSourceOrder[out[j].Source]
		}
		return out[i].Currency < out[j].Currency
	})
	return out
}
