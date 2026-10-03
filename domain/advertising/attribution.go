package advertising

import "time"

type Attribution struct {
	AdMetaID        string
	Conversations   int64
	Leads           int64
	WonDeals        int64
	Revenue         int64
	RevenueCurrency string
}

type Outcome struct {
	Conversations       int64
	Leads               int64
	WonDeals            int64
	RevenueMicros       int64
	CostPerConversation *int64
	CostPerLead         *int64
	ROAS                *float64
}

const centsToMicros = 10_000

func OutcomeOf(spend Metrics, a Attribution) Outcome {
	out := Outcome{Conversations: a.Conversations, Leads: a.Leads, WonDeals: a.WonDeals}
	if spend.Empty() {
		return out
	}
	out.CostPerConversation = ratio(spend.SpendMicros, a.Conversations)
	out.CostPerLead = ratio(spend.SpendMicros, a.Leads)
	if a.WonDeals == 0 || a.RevenueCurrency != spend.Currency {
		return out
	}
	out.RevenueMicros = a.Revenue * centsToMicros
	if spend.SpendMicros > 0 {
		roas := float64(out.RevenueMicros) / float64(spend.SpendMicros)
		out.ROAS = &roas
	}
	return out
}

func SumAttributions(rows []Attribution) Attribution {
	var total Attribution
	for _, row := range rows {
		total.Conversations += row.Conversations
		total.Leads += row.Leads
		if row.WonDeals == 0 {
			continue
		}
		if total.RevenueCurrency != "" && total.RevenueCurrency != row.RevenueCurrency {
			total.RevenueCurrency = "MIXED"
		} else if total.RevenueCurrency == "" {
			total.RevenueCurrency = row.RevenueCurrency
		}
		total.WonDeals += row.WonDeals
		total.Revenue += row.Revenue
	}
	return total
}

type LeadCost struct {
	AdMetaID      string
	Day           time.Time
	Currency      string
	SpendMicros   int64
	Conversations int64
}

func (c LeadCost) Estimate() *int64 { return ratio(c.SpendMicros, c.Conversations) }
