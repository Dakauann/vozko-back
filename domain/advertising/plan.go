package advertising

import "time"

type CampaignSpec struct {
	Name              string
	ProductCatalogID  string
	Objective         Objective
	SpecialCategories []SpecialCategory
	Budget            *Budget
	Bid               Bid
	Status            ConfiguredStatus
}

type PromotedObject struct {
	PageID              string
	WhatsAppPhoneNumber string
	PixelID             string
	PixelEvent          PixelEvent
	AppID               string
	AppStoreURL         string
	ProductSetID        string
}

type AdSetSpec struct {
	Name              string
	DynamicCreative   bool
	CampaignID        string
	Budget            *Budget
	Bid               Bid
	CampaignBidAmount int64
	BillingEvent      string
	Goal              OptimizationGoal
	Destination       Destination
	PromotedObject    PromotedObject
	Targeting         Targeting
	Placements        Placements
	StartTime         *time.Time
	EndTime           *time.Time
	Schedule          []DayPart
	Status            ConfiguredStatus
}

type UploadedMedia struct {
	ImageHashes map[string]string
	VideoIDs    map[string]string
}

func (u UploadedMedia) ImageHash(mediaID string) string { return u.ImageHashes[mediaID] }

func (u UploadedMedia) VideoID(mediaID string) string { return u.VideoIDs[mediaID] }

type CreativeAssetMode string

const (
	AssetsSingle  CreativeAssetMode = ""
	AssetsDynamic CreativeAssetMode = "dynamic"
	AssetsGroups  CreativeAssetMode = "groups"
)

type CreativeSpec struct {
	Name         string
	AssetMode    CreativeAssetMode
	Identity     Identity
	Destination  Destination
	CallToAction CallToAction
	Creative     CreativeDraft
	Media        UploadedMedia
	AppStoreURL  string
	ProductSetID string
}

type AdSpec struct {
	Name        string
	AdSetID     string
	CreativeID  string
	Status      ConfiguredStatus
	AssetGroups *CreativeSpec
}

func CampaignSpecOf(d AdDraft) CampaignSpec {
	spec := CampaignSpec{Name: d.Campaign.Name, Objective: d.Campaign.Objective, Budget: d.Campaign.Budget, Status: StatusPaused}
	if d.Campaign.Budget != nil {
		spec.Bid = Bid{Strategy: d.Campaign.Bid.Strategy}
	}
	if d.AdSet.Destination == DestinationCatalog {
		spec.ProductCatalogID = d.AdSet.CatalogID
	}
	if d.Campaign.SpecialCategory != CategoryNone && d.Campaign.SpecialCategory != "" {
		spec.SpecialCategories = []SpecialCategory{d.Campaign.SpecialCategory}
	}
	return spec
}

func AdSetSpecOf(d AdDraft, campaignID string) AdSetSpec {
	s := d.AdSet
	spec := AdSetSpec{
		Name:            s.Name,
		CampaignID:      campaignID,
		BillingEvent:    s.Goal.BillingEvent(),
		Goal:            s.Goal,
		Destination:     s.Destination,
		Targeting:       s.Targeting,
		Placements:      s.Placements,
		StartTime:       s.StartAt,
		EndTime:         s.EndAt,
		Schedule:        s.Schedule,
		Status:          StatusPaused,
		PromotedObject:  promotedObjectFor(d),
		DynamicCreative: d.DynamicCreative(),
	}
	if !d.CampaignBudget() {
		spec.Budget = s.Budget
		spec.Bid = s.Bid
	} else if d.Campaign.Bid.NeedsAmount() {
		spec.CampaignBidAmount = d.Campaign.Bid.Amount
	}
	return spec
}

func promotedObjectFor(d AdDraft) PromotedObject {
	s := d.AdSet
	var p PromotedObject
	switch s.Destination {
	case DestinationWhatsApp:
		p.PageID, p.WhatsAppPhoneNumber = d.Identity.PageID, s.WhatsAppNumber
	case DestinationMessenger, DestinationInstagramDirect, DestinationInstantForm, DestinationOnPost, DestinationNone:
		p.PageID = d.Identity.PageID
	case DestinationApp:
		p.AppID, p.AppStoreURL = s.AppID, s.AppStoreURL
	case DestinationCatalog:
		p.ProductSetID = s.ProductSetID
		p.PixelEvent = s.PixelEvent
		if p.PixelEvent == "" {
			p.PixelEvent = EventPurchase
		}
		return p
	}
	if s.Goal.NeedsPixel() {
		p.PixelID, p.PixelEvent = s.PixelID, s.PixelEvent
	}
	return p
}

func CreativeSpecOf(d AdDraft, index int, media UploadedMedia) CreativeSpec {
	ad := d.Ads[index]
	return CreativeSpec{
		Name:         ad.Name,
		Identity:     d.Identity,
		Destination:  d.AdSet.Destination,
		CallToAction: ad.Creative.ResolvedCallToAction(d.AdSet.Destination),
		Creative:     ad.Creative,
		Media:        media,
		AppStoreURL:  d.AdSet.AppStoreURL,
		ProductSetID: d.AdSet.ProductSetID,
		AssetMode:    assetModeOf(d, ad.Creative),
	}
}

func assetModeOf(d AdDraft, c CreativeDraft) CreativeAssetMode {
	if c.Format != FormatFlexible {
		return AssetsSingle
	}
	if d.Campaign.Objective.AssetGroups() {
		return AssetsGroups
	}
	return AssetsDynamic
}

func AdSpecOf(d AdDraft, index int, adSetID, creativeID string, media UploadedMedia) AdSpec {
	spec := AdSpec{Name: d.Ads[index].Name, AdSetID: adSetID, CreativeID: creativeID, Status: StatusPaused}
	if creative := CreativeSpecOf(d, index, media); creative.AssetMode == AssetsGroups {
		spec.AssetGroups = &creative
	}
	return spec
}
