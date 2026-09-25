package attendance

import "sort"

const (
	ReasonRevenueMixedCurrencies     = "revenue_mixed_currencies"
	ReasonNoRevenueRepository        = "revenue_repository_unavailable"
)

type RevenueTally struct {
	Currency        string
	OwnerID         string
	WonCount        int64
	ValueCents      int64
	WonWithoutValue int64
}

type RevenueByCurrency struct {
	Currency   string   `json:"currency"`
	ValueCents int64    `json:"value_cents"`
	WonCount   int64    `json:"won_count"`
	AvgTicket  *float64 `json:"avg_ticket_cents"`
	PerOpenDay *float64 `json:"per_open_day_cents"`
	Projected  *int64   `json:"projected_cents"`
	PrevClosed *int64   `json:"prev_closed_cents"`
	DeltaPct   *float64 `json:"delta_pct"`
}

type RevenueOwnerRow struct {
	OwnerID    string   `json:"owner_id"`
	Currency   string   `json:"currency"`
	WonCount   int64    `json:"won_count"`
	ValueCents int64    `json:"value_cents"`
	AvgTicket  *float64 `json:"avg_ticket_cents"`
}

type Revenue struct {
	Currencies      []RevenueByCurrency `json:"currencies"`
	ByOwner         []RevenueOwnerRow   `json:"by_owner"`
	BySource        []RevenueSourceRow  `json:"by_source"`
	WonWithoutValue int64               `json:"won_without_value"`
	Unattributed    int64               `json:"unattributed"`
	UnownedCount    int64               `json:"unowned_count"`
	MixedCurrencies bool                `json:"mixed_currencies"`
	Available       bool                `json:"available"`
	Reason          string              `json:"reason,omitempty"`
}

func (r Revenue) WonCount() int64 {
	var total int64
	for _, c := range r.Currencies {
		total += c.WonCount
	}
	return total
}

func (r Revenue) SingleCurrencyCents() int64 {
	if len(r.Currencies) != 1 {
		return 0
	}
	return r.Currencies[0].ValueCents
}

func UnavailableRevenue(reason string) Revenue {
	return Revenue{
		Currencies: []RevenueByCurrency{},
		ByOwner:    []RevenueOwnerRow{},
		BySource:   []RevenueSourceRow{},
		Reason:     reason,
	}
}

func BuildRevenue(tallies []RevenueTally, unattributed int64, p Period, prevByCurrency map[string]int64) Revenue {
	out := Revenue{
		Currencies:   []RevenueByCurrency{},
		ByOwner:      []RevenueOwnerRow{},
		BySource:     revenueBySource(tallies),
		Unattributed: clampNonNegative(unattributed),
		Available:    true,
	}

	byCurrency := map[string]*RevenueByCurrency{}
	for _, t := range tallies {
		currency := t.Currency
		if currency == "" {
			continue
		}
		row, found := byCurrency[currency]
		if !found {
			row = &RevenueByCurrency{Currency: currency}
			byCurrency[currency] = row
		}
		row.ValueCents += t.ValueCents
		row.WonCount += t.WonCount
		out.WonWithoutValue += t.WonWithoutValue

		if t.OwnerID == "" {
			out.UnownedCount += t.WonCount
		}
		out.ByOwner = append(out.ByOwner, RevenueOwnerRow{
			OwnerID:    t.OwnerID,
			Currency:   currency,
			WonCount:   t.WonCount,
			ValueCents: t.ValueCents,
			AvgTicket:  avgTicket(t.ValueCents, t.WonCount),
		})
	}

	for _, row := range byCurrency {
		row.AvgTicket = avgTicket(row.ValueCents, row.WonCount)
		if p.Available && p.OpenDaysDone >= MinOpenDaysForProjection && p.ElapsedPct >= MinElapsedPctForProjection {
			perDay := float64(row.ValueCents) / float64(p.OpenDaysDone)
			if isFinite(perDay) {
				row.PerOpenDay = round2Ptr(perDay)
				projected := int64(perDay * float64(p.OpenDaysTotal))
				row.Projected = &projected
			}
		}
		if prev, found := prevByCurrency[row.Currency]; found {
			previous := prev
			row.PrevClosed = &previous
			reference := float64(row.ValueCents)
			if row.Projected != nil {
				reference = float64(*row.Projected)
			}
			if delta, ok := ratioPct(reference-float64(prev), float64(prev)); ok {
				row.DeltaPct = &delta
			}
		}
		out.Currencies = append(out.Currencies, *row)
	}

	sort.Slice(out.Currencies, func(i, j int) bool {
		if out.Currencies[i].ValueCents != out.Currencies[j].ValueCents {
			return out.Currencies[i].ValueCents > out.Currencies[j].ValueCents
		}
		return out.Currencies[i].Currency < out.Currencies[j].Currency
	})
	sort.Slice(out.ByOwner, func(i, j int) bool {
		if out.ByOwner[i].ValueCents != out.ByOwner[j].ValueCents {
			return out.ByOwner[i].ValueCents > out.ByOwner[j].ValueCents
		}
		return out.ByOwner[i].OwnerID < out.ByOwner[j].OwnerID
	})

	out.MixedCurrencies = len(out.Currencies) > 1
	if out.MixedCurrencies {
		out.Reason = ReasonRevenueMixedCurrencies
	}
	return out
}

func avgTicket(valueCents, wonCount int64) *float64 {
	if wonCount <= 0 {
		return nil
	}
	return round2Ptr(float64(valueCents) / float64(wonCount))
}

func RevenueForOwner(tallies []RevenueTally, ownerID string) []RevenueTally {
	if ownerID == "" {
		return tallies
	}
	out := make([]RevenueTally, 0, len(tallies))
	for _, tally := range tallies {
		if tally.OwnerID == ownerID {
			out = append(out, tally)
		}
	}
	return out
}
