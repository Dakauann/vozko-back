package advertisinghttp

import (
	"time"

	"vozko/domain/advertising"
)

type AccountResponse struct {
	ID            string     `json:"id"`
	MetaAccountID string     `json:"metaAccountId"`
	Name          string     `json:"name"`
	BusinessName  string     `json:"businessName,omitempty"`
	Currency      string     `json:"currency"`
	Timezone      string     `json:"timezone"`
	MetaStatus    string     `json:"metaStatus"`
	Connection    string     `json:"connection"`
	HasFunding    bool       `json:"hasFunding"`
	CanCreate     bool       `json:"canCreate"`
	CanSpend      bool       `json:"canSpend"`
	SpendBlocker  string     `json:"spendBlocker,omitempty"`
	SpendCap      *int64     `json:"spendCap"`
	AmountSpent   int64      `json:"amountSpent"`
	LastSyncedAt  *time.Time `json:"lastSyncedAt,omitempty"`
}

type MetricsResponse struct {
	Currency            string   `json:"currency"`
	Spend               int64    `json:"spend"`
	Impressions         int64    `json:"impressions"`
	Clicks              int64    `json:"clicks"`
	LinkClicks          int64    `json:"linkClicks"`
	Results             int64    `json:"results"`
	ResultAction        string   `json:"resultAction"`
	MixedResults        bool     `json:"mixedResults"`
	CostPerResult       *int64   `json:"costPerResult"`
	Conversations       int64    `json:"conversations"`
	CostPerConversation *int64   `json:"costPerConversation"`
	CPC                 *int64   `json:"cpc"`
	CPM                 *int64   `json:"cpm"`
	CTR                 *float64 `json:"ctr"`
}

type OutcomeResponse struct {
	Conversations       int64    `json:"conversations"`
	Leads               int64    `json:"leads"`
	WonDeals            int64    `json:"wonDeals"`
	Revenue             int64    `json:"revenue"`
	CostPerConversation *int64   `json:"costPerConversation"`
	CostPerLead         *int64   `json:"costPerLead"`
	ROAS                *float64 `json:"roas"`
}

type CreativeResponse struct {
	Title        string `json:"title,omitempty"`
	Body         string `json:"body,omitempty"`
	ImageURL     string `json:"imageUrl,omitempty"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}

type RowResponse struct {
	MetaID           string              `json:"metaId"`
	Level            string              `json:"level"`
	Name             string              `json:"name"`
	CampaignID       string              `json:"campaignId,omitempty"`
	AdSetID          string              `json:"adSetId,omitempty"`
	Status           string              `json:"status"`
	EffectiveStatus  string              `json:"effectiveStatus"`
	Delivery         string              `json:"delivery"`
	IsOn             bool                `json:"isOn"`
	CanToggle        bool                `json:"canToggle"`
	Objective        string              `json:"objective,omitempty"`
	OptimizationGoal string              `json:"optimizationGoal,omitempty"`
	DestinationType  string              `json:"destinationType,omitempty"`
	DailyBudget      int64               `json:"dailyBudget"`
	LifetimeBudget   int64               `json:"lifetimeBudget"`
	StartTime        *time.Time          `json:"startTime,omitempty"`
	EndTime          *time.Time          `json:"endTime,omitempty"`
	Creative         *CreativeResponse   `json:"creative,omitempty"`
	Issues           []advertising.Issue `json:"issues"`
	ReviewFeedback   map[string]string   `json:"reviewFeedback,omitempty"`
	Metrics          MetricsResponse     `json:"metrics"`
	Outcome          OutcomeResponse     `json:"outcome"`
}

type RangeResponse struct {
	Since string `json:"since"`
	Until string `json:"until"`
}

type MetaIDResponse struct {
	MetaID string `json:"metaId"`
}
