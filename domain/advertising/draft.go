package advertising

import (
	"strconv"
	"strings"
	"time"
)

type Destination string

const (
	DestinationWhatsApp        Destination = "WHATSAPP"
	DestinationMessenger       Destination = "MESSENGER"
	DestinationInstagramDirect Destination = "INSTAGRAM_DIRECT"
)

type SpecialCategory string

const (
	CategoryNone       SpecialCategory = "NONE"
	CategoryHousing    SpecialCategory = "HOUSING"
	CategoryEmployment SpecialCategory = "EMPLOYMENT"
	CategoryFinancial  SpecialCategory = "FINANCIAL_PRODUCTS_SERVICES"
	CategoryPolitics   SpecialCategory = "ISSUES_ELECTIONS_POLITICS"
	CategoryGambling   SpecialCategory = "ONLINE_GAMBLING_AND_GAMING"
)

func (c SpecialCategory) Restricted() bool {
	return c == CategoryHousing || c == CategoryEmployment || c == CategoryFinancial
}

const (
	GoalConversations       OptimizationGoal = "CONVERSATIONS"
	BillingImpressions                       = "IMPRESSIONS"
	BidLowestCostWithoutCap                  = "LOWEST_COST_WITHOUT_CAP"
)

type CampaignDraft struct {
	ExistingID      string          `json:"existingId,omitempty"`
	Name            string          `json:"name,omitempty"`
	Objective       Objective       `json:"objective,omitempty"`
	SpecialCategory SpecialCategory `json:"specialCategory,omitempty"`
	Budget          *Budget         `json:"budget,omitempty"`
	Bid             Bid             `json:"bid,omitempty"`
}

type AdSetDraft struct {
	ExistingID     string           `json:"existingId,omitempty"`
	Name           string           `json:"name,omitempty"`
	Destination    Destination      `json:"destination,omitempty"`
	Goal           OptimizationGoal `json:"goal,omitempty"`
	WhatsAppNumber string           `json:"whatsAppNumber,omitempty"`
	PixelID        string           `json:"pixelId,omitempty"`
	PixelEvent     PixelEvent       `json:"pixelEvent,omitempty"`
	AppID          string           `json:"appId,omitempty"`
	AppStoreURL    string           `json:"appStoreUrl,omitempty"`
	CatalogID      string           `json:"catalogId,omitempty"`
	ProductSetID   string           `json:"productSetId,omitempty"`
	Budget         *Budget          `json:"budget,omitempty"`
	Bid            Bid              `json:"bid,omitempty"`
	StartAt        *time.Time       `json:"startAt,omitempty"`
	EndAt          *time.Time       `json:"endAt,omitempty"`
	Schedule       []DayPart        `json:"schedule,omitempty"`
	Targeting      Targeting        `json:"targeting"`
	Placements     Placements       `json:"placements"`
}

type Identity struct {
	PageID          string `json:"pageId"`
	InstagramUserID string `json:"instagramUserId,omitempty"`
}

type AdItem struct {
	Name     string        `json:"name,omitempty"`
	Creative CreativeDraft `json:"creative"`
}

type AdDraft struct {
	AdAccountID string        `json:"adAccountId"`
	Identity    Identity      `json:"identity"`
	Campaign    CampaignDraft `json:"campaign"`
	AdSet       AdSetDraft    `json:"adSet"`
	Ads         []AdItem      `json:"ads"`
	KeepPaused  bool          `json:"keepPaused,omitempty"`
}

type ExistingParents struct {
	Campaign *Object
	AdSet    *Object
}

const (
	maxNameRunes   = 400
	maxAdsPerDraft = 10
)

func (d *AdDraft) Normalize() {
	d.AdAccountID = strings.TrimSpace(d.AdAccountID)
	d.Identity.PageID = strings.TrimSpace(d.Identity.PageID)
	d.Identity.InstagramUserID = strings.TrimSpace(d.Identity.InstagramUserID)
	c := &d.Campaign
	c.ExistingID = strings.TrimSpace(c.ExistingID)
	c.Name = strings.TrimSpace(c.Name)
	if c.SpecialCategory == "" {
		c.SpecialCategory = CategoryNone
	}
	c.Bid = c.Bid.Normalized()
	s := &d.AdSet
	s.ExistingID = strings.TrimSpace(s.ExistingID)
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = c.Name
	}
	s.WhatsAppNumber = DigitsOnly(s.WhatsAppNumber)
	s.Bid = s.Bid.Normalized()
	s.Targeting.Normalize()
	if !s.Placements.Automatic && len(s.Placements.Platforms) == 0 {
		s.Placements = Placements{Automatic: true}
	}
	for i := range d.Ads {
		d.Ads[i].Name = strings.TrimSpace(d.Ads[i].Name)
		if d.Ads[i].Name == "" {
			d.Ads[i].Name = defaultAdName(c.Name, s.Name, i, len(d.Ads))
		}
		d.Ads[i].Creative.Normalize()
	}
}

func defaultAdName(campaign, adSet string, index, total int) string {
	base := campaign
	if base == "" {
		base = adSet
	}
	if total <= 1 {
		return base
	}
	return base + " " + strconv.Itoa(index+1)
}

func (d *AdDraft) Adopt(parents ExistingParents) {
	if parents.Campaign != nil {
		d.Campaign.Objective = Objective(parents.Campaign.Objective)
		d.Campaign.Name = parents.Campaign.Name
		if parents.Campaign.SpecialCategory != "" {
			d.Campaign.SpecialCategory = parents.Campaign.SpecialCategory
		}
		if parents.Campaign.DailyBudget > 0 {
			d.Campaign.Budget = &Budget{Kind: BudgetDaily, Amount: parents.Campaign.DailyBudget}
		} else if parents.Campaign.LifetimeBudget > 0 {
			d.Campaign.Budget = &Budget{Kind: BudgetLifetime, Amount: parents.Campaign.LifetimeBudget}
		}
	}
	if parents.AdSet != nil {
		d.AdSet.Goal = OptimizationGoal(parents.AdSet.OptimizationGoal)
		d.AdSet.Destination = d.Campaign.Objective.DestinationOf(parents.AdSet.DestinationType, d.AdSet.Goal)
		d.Campaign.ExistingID = parents.AdSet.CampaignMetaID
	}
}

func (d AdDraft) AdsToPublish() int { return len(d.Ads) }

func (d AdDraft) NeedsLeadTerms() bool { return d.AdSet.Destination == DestinationInstantForm }

func (d AdDraft) NewCampaign() bool { return d.Campaign.ExistingID == "" }

func (d AdDraft) NewAdSet() bool { return d.AdSet.ExistingID == "" }

func (d AdDraft) NewAdSetBudget() *Budget {
	if !d.NewAdSet() {
		return nil
	}
	return d.AdSet.Budget
}

func (d AdDraft) CampaignBudget() bool { return d.Campaign.Budget != nil }

func (d AdDraft) Validate(now time.Time) error {
	v := newIssues()
	if d.AdAccountID == "" {
		v.add("adAccountId", "required")
	}
	if d.Identity.PageID == "" {
		v.at("identity").add("pageId", "required")
	}
	if !d.Campaign.Objective.Valid() {
		v.at("campaign").add("objective", "invalid")
		return v.err()
	}
	if d.NewCampaign() {
		d.validateCampaign(v.at("campaign"))
	}
	if d.NewAdSet() {
		d.validateAdSet(v.at("adSet"), now)
	}
	switch n := len(d.Ads); {
	case n == 0:
		v.add("ads", "required")
	case n > maxAdsPerDraft:
		v.add("ads", "too_many")
	}
	d.validateDynamicCreative(v)
	for i, ad := range d.Ads {
		av := v.item("ads", i)
		av.text("name", ad.Name, true, maxNameRunes)
		ad.Creative.validate(av.at("creative"), d.AdSet.Destination)
	}
	return v.err()
}

func (d AdDraft) validateCampaign(v issues) {
	c := d.Campaign
	v.text("name", c.Name, true, maxNameRunes)
	switch {
	case c.SpecialCategory == CategoryPolitics:
		v.add("specialCategory", "political_not_supported")
	case c.SpecialCategory != CategoryNone && !c.SpecialCategory.Restricted():
		v.add("specialCategory", "invalid")
	}
	if c.Budget != nil {
		c.Budget.validate(v.at("budget"))
		c.Bid.validate(v.at("bid"), d.AdSet.Goal)
		c.Bid.validateOnCampaign(v.at("bid"))
	} else if c.Bid.Strategy != BidLowestCost {
		v.at("bid").add("strategy", "needs_campaign_budget")
	}
}

func (d AdDraft) validateAdSet(v issues, now time.Time) {
	s := d.AdSet
	v.text("name", s.Name, true, maxNameRunes)
	if !d.Campaign.Objective.Allows(s.Destination, s.Goal) {
		v.add("goal", "not_for_objective")
	}
	budget := s.Budget
	switch {
	case d.CampaignBudget() && s.Budget != nil:
		v.add("budget", "campaign_has_budget")
		budget = d.Campaign.Budget
	case d.CampaignBudget():
		budget = d.Campaign.Budget
		if s.Bid.Strategy != BidLowestCost {
			v.at("bid").add("strategy", "set_on_campaign")
		}
	case s.Budget == nil:
		v.add("budget", "required")
	default:
		s.Budget.validate(v.at("budget"))
		s.Bid.validate(v.at("bid"), s.Goal)
	}
	validateFlight(v, s.StartAt, s.EndAt, budget, now)
	validateSchedule(v.at("schedule"), s.Schedule, budget)
	s.Targeting.validate(v.at("targeting"), d.Campaign.SpecialCategory.Restricted())
	s.Placements.validate(v.at("placements"), s.Destination)
	d.validatePromotion(v)
}

func (d AdDraft) validatePromotion(v issues) {
	s := d.AdSet
	switch s.Destination {
	case DestinationWhatsApp:
		if n := len(s.WhatsAppNumber); n < 8 || n > 15 {
			v.add("whatsAppNumber", "required")
		}
	case DestinationInstagramDirect:
		if d.Identity.InstagramUserID == "" {
			v.add("instagramUserId", "required")
		}
	case DestinationApp:
		if strings.TrimSpace(s.AppID) == "" {
			v.add("appId", "required")
		}
		v.url("appStoreUrl", s.AppStoreURL, true)
	case DestinationCatalog:
		if strings.TrimSpace(s.CatalogID) == "" || strings.TrimSpace(s.ProductSetID) == "" {
			v.add("productSetId", "required")
		}
	}
	if s.Goal.NeedsPixel() && s.Destination != DestinationCatalog {
		if strings.TrimSpace(s.PixelID) == "" {
			v.add("pixelId", "required")
		}
		if !s.PixelEvent.Valid() {
			v.add("pixelEvent", "invalid")
		}
	}
}

func DigitsOnly(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func SameWhatsAppNumber(a, b string) bool {
	da, db := DigitsOnly(a), DigitsOnly(b)
	return da != "" && da == db
}

func (d AdDraft) DynamicCreative() bool {
	if d.Campaign.Objective.AssetGroups() {
		return false
	}
	for _, ad := range d.Ads {
		if ad.Creative.Format == FormatFlexible {
			return true
		}
	}
	return false
}

func (d AdDraft) validateDynamicCreative(v issues) {
	if !d.DynamicCreative() {
		return
	}
	if !d.NewAdSet() || len(d.Ads) != 1 {
		v.add("ads", "dynamic_creative_needs_own_ad_set")
	}
}
