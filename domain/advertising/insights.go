package advertising

import (
	"fmt"
	"time"
)

const (
	ActionConversationStarted = "onsite_conversion.messaging_conversation_started_7d"
	ActionLead                = "lead"
	ActionLinkClick           = "link_click"
	ActionLandingPageView     = "landing_page_view"
	ActionPostEngagement      = "post_engagement"
	ResultImpressions         = "impressions"
)

var resultActionByGoal = map[string]string{
	"CONVERSATIONS":      ActionConversationStarted,
	"LEAD_GENERATION":    ActionLead,
	"QUALITY_LEAD":       ActionLead,
	"LINK_CLICKS":        ActionLinkClick,
	"LANDING_PAGE_VIEWS": ActionLandingPageView,
	"POST_ENGAGEMENT":    ActionPostEngagement,
	"IMPRESSIONS":        ResultImpressions,
}

func ResultActionFor(optimizationGoal string) string {
	return resultActionByGoal[optimizationGoal]
}

func InsightActionTypes() []string {
	return []string{ActionConversationStarted, ActionLead, ActionLinkClick, ActionLandingPageView, ActionPostEngagement}
}

type DailyInsight struct {
	AdAccountID    string
	CampaignMetaID string
	AdSetMetaID    string
	AdMetaID       string
	Day            time.Time
	Currency       string
	SpendMicros    int64
	Impressions    int64
	Clicks         int64
	LinkClicks     int64
	Actions        map[string]int64
	FetchedAt      time.Time
}

type Metrics struct {
	Currency      string
	SpendMicros   int64
	Impressions   int64
	Clicks        int64
	LinkClicks    int64
	Conversations int64
	Leads         int64
	Results       int64
	ResultAction  string
	MixedResults  bool
}

func MetricsOf(row DailyInsight, optimizationGoal string) Metrics {
	m := Metrics{
		Currency:      row.Currency,
		SpendMicros:   row.SpendMicros,
		Impressions:   row.Impressions,
		Clicks:        row.Clicks,
		LinkClicks:    row.LinkClicks,
		Conversations: row.Actions[ActionConversationStarted],
		Leads:         row.Actions[ActionLead],
		ResultAction:  ResultActionFor(optimizationGoal),
	}
	switch m.ResultAction {
	case "":
	case ResultImpressions:
		m.Results = row.Impressions
	default:
		m.Results = row.Actions[m.ResultAction]
	}
	return m
}

func (m Metrics) Empty() bool {
	return m.Currency == "" && m.SpendMicros == 0 && m.Impressions == 0
}

func (m Metrics) Add(other Metrics) (Metrics, error) {
	if other.Empty() {
		return m, nil
	}
	if m.Empty() {
		return other, nil
	}
	if m.Currency != other.Currency {
		return Metrics{}, fmt.Errorf("%w: %s and %s", ErrMixedCurrencies, m.Currency, other.Currency)
	}
	sum := Metrics{
		Currency:      m.Currency,
		SpendMicros:   m.SpendMicros + other.SpendMicros,
		Impressions:   m.Impressions + other.Impressions,
		Clicks:        m.Clicks + other.Clicks,
		LinkClicks:    m.LinkClicks + other.LinkClicks,
		Conversations: m.Conversations + other.Conversations,
		Leads:         m.Leads + other.Leads,
		ResultAction:  m.ResultAction,
		MixedResults:  m.MixedResults || other.MixedResults || m.ResultAction != other.ResultAction,
	}
	if sum.MixedResults {
		sum.ResultAction = ""
		return sum, nil
	}
	sum.Results = m.Results + other.Results
	return sum, nil
}

func SumMetrics(rows []Metrics) (Metrics, error) {
	var total Metrics
	for _, row := range rows {
		next, err := total.Add(row)
		if err != nil {
			return Metrics{}, err
		}
		total = next
	}
	return total, nil
}

func ratio(numerator, denominator int64) *int64 {
	if denominator <= 0 {
		return nil
	}
	v := numerator / denominator
	return &v
}

func (m Metrics) CostPerResult() *int64 {
	if m.ResultAction == "" {
		return nil
	}
	return ratio(m.SpendMicros, m.Results)
}

func (m Metrics) CostPerConversation() *int64 { return ratio(m.SpendMicros, m.Conversations) }

func (m Metrics) CostPerLinkClick() *int64 { return ratio(m.SpendMicros, m.LinkClicks) }

func (m Metrics) CPM() *int64 { return ratio(m.SpendMicros*1000, m.Impressions) }

func (m Metrics) CTR() *float64 {
	if m.Impressions <= 0 {
		return nil
	}
	v := float64(m.LinkClicks) / float64(m.Impressions) * 100
	return &v
}
