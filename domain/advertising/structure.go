package advertising

import (
	"fmt"
	"time"
)

type Level string

const (
	LevelCampaign Level = "campaign"
	LevelAdSet    Level = "adset"
	LevelAd       Level = "ad"
)

func (l Level) Valid() bool {
	return l == LevelCampaign || l == LevelAdSet || l == LevelAd
}

func (l Level) OrCampaign() Level {
	if l == "" {
		return LevelCampaign
	}
	return l
}

type ConfiguredStatus string

const (
	StatusActive   ConfiguredStatus = "ACTIVE"
	StatusPaused   ConfiguredStatus = "PAUSED"
	StatusDeleted  ConfiguredStatus = "DELETED"
	StatusArchived ConfiguredStatus = "ARCHIVED"
)

type EffectiveStatus string

const (
	EffectiveActive         EffectiveStatus = "ACTIVE"
	EffectivePaused         EffectiveStatus = "PAUSED"
	EffectiveDeleted        EffectiveStatus = "DELETED"
	EffectiveArchived       EffectiveStatus = "ARCHIVED"
	EffectivePendingReview  EffectiveStatus = "PENDING_REVIEW"
	EffectiveInProcess      EffectiveStatus = "IN_PROCESS"
	EffectivePreapproved    EffectiveStatus = "PREAPPROVED"
	EffectiveDisapproved    EffectiveStatus = "DISAPPROVED"
	EffectiveWithIssues     EffectiveStatus = "WITH_ISSUES"
	EffectivePendingBilling EffectiveStatus = "PENDING_BILLING_INFO"
	EffectiveCampaignPaused EffectiveStatus = "CAMPAIGN_PAUSED"
	EffectiveAdSetPaused    EffectiveStatus = "ADSET_PAUSED"
)

var ReviewStatuses = []EffectiveStatus{EffectivePendingReview, EffectiveInProcess, EffectivePreapproved}

type Delivery string

const (
	DeliveryActive         Delivery = "active"
	DeliveryScheduled      Delivery = "scheduled"
	DeliveryCompleted      Delivery = "completed"
	DeliveryOff            Delivery = "off"
	DeliveryCampaignOff    Delivery = "campaign_off"
	DeliveryAdSetOff       Delivery = "adset_off"
	DeliveryInReview       Delivery = "in_review"
	DeliveryRejected       Delivery = "rejected"
	DeliveryWithIssues     Delivery = "with_issues"
	DeliveryPendingBilling Delivery = "pending_billing"
	DeliveryArchived       Delivery = "archived"
	DeliveryDeleted        Delivery = "deleted"
	DeliveryUnknown        Delivery = "unknown"
)

type Issue struct {
	Code    int    `json:"code"`
	Summary string `json:"summary"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

type Creative struct {
	ID           string `json:"id,omitempty"`
	Title        string `json:"title,omitempty"`
	Body         string `json:"body,omitempty"`
	ImageURL     string `json:"imageUrl,omitempty"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}

type Object struct {
	MetaID           string
	WorkspaceID      string
	AdAccountID      string
	Level            Level
	CampaignMetaID   string
	AdSetMetaID      string
	Name             string
	Status           ConfiguredStatus
	EffectiveStatus  EffectiveStatus
	Objective        string
	SpecialCategory  SpecialCategory
	DestinationType  string
	OptimizationGoal string
	BidStrategy      string
	DailyBudget      int64
	LifetimeBudget   int64
	BudgetRemaining  int64
	StartTime        *time.Time
	EndTime          *time.Time
	Creative         *Creative
	ReviewFeedback   map[string]string
	Issues           []Issue
	BudgetChanges    []time.Time
	CreatedTime      *time.Time
	UpdatedTime      *time.Time
	FirstDeliveredAt *time.Time
	SyncedAt         time.Time
}

func (o *Object) ParentMetaID() string {
	switch o.Level {
	case LevelAdSet:
		return o.CampaignMetaID
	case LevelAd:
		return o.AdSetMetaID
	}
	return ""
}

func (o *Object) Delivery(now time.Time) Delivery {
	switch o.EffectiveStatus {
	case EffectiveActive:
		if o.EndTime != nil && !now.Before(*o.EndTime) {
			return DeliveryCompleted
		}
		if o.StartTime != nil && now.Before(*o.StartTime) {
			return DeliveryScheduled
		}
		return DeliveryActive
	case EffectivePaused:
		return DeliveryOff
	case EffectiveCampaignPaused:
		return DeliveryCampaignOff
	case EffectiveAdSetPaused:
		return DeliveryAdSetOff
	case EffectivePendingReview, EffectiveInProcess, EffectivePreapproved:
		return DeliveryInReview
	case EffectiveDisapproved:
		return DeliveryRejected
	case EffectiveWithIssues:
		return DeliveryWithIssues
	case EffectivePendingBilling:
		return DeliveryPendingBilling
	case EffectiveArchived:
		return DeliveryArchived
	case EffectiveDeleted:
		return DeliveryDeleted
	}
	return DeliveryUnknown
}

func (o *Object) Delivered() bool { return o.FirstDeliveredAt != nil }

func (o *Object) Locked() bool {
	return o.Status == StatusDeleted || o.Status == StatusArchived ||
		o.EffectiveStatus == EffectiveDeleted || o.EffectiveStatus == EffectiveArchived
}

func (o *Object) IsOn() bool { return o.Status == StatusActive }

func (o *Object) CanToggle() error {
	if o.Locked() {
		return ErrObjectLocked
	}
	return nil
}

const (
	maxBudgetChangesPerHour = 4
	budgetChangeWindow      = time.Hour
)

func (o *Object) Budget() *Budget {
	switch {
	case o.Level == LevelAd:
		return nil
	case o.DailyBudget > 0:
		return &Budget{Kind: BudgetDaily, Amount: o.DailyBudget}
	case o.LifetimeBudget > 0:
		return &Budget{Kind: BudgetLifetime, Amount: o.LifetimeBudget}
	}
	return nil
}

func (o *Object) CanChangeBudget(now time.Time) error {
	if o.Locked() {
		return ErrObjectLocked
	}
	if o.Budget() == nil {
		return ErrNoBudget
	}
	if len(o.recentBudgetChanges(now)) >= maxBudgetChangesPerHour {
		return ErrBudgetChangeTooSoon
	}
	return nil
}

func (o *Object) RecordBudgetChange(now time.Time) {
	o.BudgetChanges = append(o.recentBudgetChanges(now), now)
}

func (o *Object) recentBudgetChanges(now time.Time) []time.Time {
	recent := make([]time.Time, 0, len(o.BudgetChanges))
	for _, at := range o.BudgetChanges {
		if now.Sub(at) < budgetChangeWindow {
			recent = append(recent, at)
		}
	}
	return recent
}

func ValidateDailyBudget(minor int64) error {
	if minor <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidBudget, minor)
	}
	return nil
}

func DeliveredObjects(rows []DailyInsight) []string {
	seen := map[string]bool{}
	var ids []string
	for _, row := range rows {
		if row.Impressions <= 0 {
			continue
		}
		for _, id := range []string{row.AdMetaID, row.AdSetMetaID, row.CampaignMetaID} {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}
