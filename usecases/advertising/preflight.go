package advertising

import (
	"context"
	"errors"
	"slices"
	"strconv"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

type Preflight struct {
	Draft         ads.AdDraft
	Account       *ads.AdAccount
	Page          ads.RemotePage
	MediaURLs     map[string]string
	Fee           Fee
	FeeTotal      Fee
	BudgetMinimum *ads.BudgetMinimum
}

type preflightGateway interface {
	ListPages(ctx context.Context, token string) ([]ads.RemotePage, error)
	ListForms(ctx context.Context, token, pageID string) ([]ads.LeadForm, error)
	ListPixels(ctx context.Context, token, metaAccountID string) ([]ads.Pixel, error)
	ListCatalogs(ctx context.Context, token, businessID string) ([]ads.RemoteCatalog, error)
	ListApps(ctx context.Context, token, metaAccountID string) ([]ads.RemoteApp, error)
	ListPagePosts(ctx context.Context, token, pageID string) ([]ads.RemotePost, error)
	ListInstagramMedia(ctx context.Context, token, instagramUserID string) ([]ads.RemotePost, error)
	ListInstantExperiences(ctx context.Context, token, pageID string) ([]ads.RemoteInstantExperience, error)
	ListAudiences(ctx context.Context, token, metaAccountID string) ([]ads.Audience, error)
	minimumGateway
}

type preflighter struct {
	access  accountAccess
	gateway preflightGateway
	objects ads.ObjectRepository
	numbers ads.NumberDirectory
	media   creativeMedia
	fees    FeeCharger
	floor   budgetFloor
}

func (p preflighter) run(ctx context.Context, workspaceID string, draft ads.AdDraft) (*Preflight, error) {
	draft.Normalize()
	if err := p.adoptParents(ctx, workspaceID, &draft); err != nil {
		return nil, err
	}
	if err := draft.Validate(p.access.now()); err != nil {
		return nil, err
	}
	account, token, err := p.access.open(ctx, workspaceID, draft.AdAccountID, ads.UseWrite)
	if err != nil {
		return nil, err
	}
	minimum, err := p.floor.check(ctx, budgetCheck{
		account: account, token: token, field: "adSet.budget.amount",
		budget: draft.NewAdSetBudget(), goal: draft.AdSet.Goal, bid: draft.AdSet.Bid,
	})
	if err != nil {
		return nil, err
	}
	page, err := p.page(ctx, token, account, draft.Identity.PageID)
	if err != nil {
		return nil, err
	}
	if err := p.checkIdentity(ctx, workspaceID, draft, page); err != nil {
		return nil, err
	}
	if err := p.checkPromotion(ctx, token, account, draft); err != nil {
		return nil, err
	}
	if err := p.checkAudiences(ctx, token, account, draft); err != nil {
		return nil, err
	}
	mediaURLs := map[string]string{}
	for i, ad := range draft.Ads {
		if err := p.checkCreative(ctx, workspaceID, token, draft, i, ad.Creative, mediaURLs); err != nil {
			return nil, err
		}
	}
	fee, err := p.fees.Quote(workspaceID)
	if err != nil {
		return nil, err
	}
	return &Preflight{Draft: draft, Account: account, Page: page, MediaURLs: mediaURLs, Fee: fee, FeeTotal: fee.Times(draft.AdsToPublish()), BudgetMinimum: minimum}, nil
}

func (p preflighter) adoptParents(ctx context.Context, workspaceID string, draft *ads.AdDraft) error {
	var parents ads.ExistingParents
	if draft.AdSet.ExistingID != "" {
		adSet, err := p.parent(ctx, workspaceID, draft.AdSet.ExistingID, ads.LevelAdSet, "adSet.existingId")
		if err != nil {
			return err
		}
		parents.AdSet = adSet
		draft.Campaign.ExistingID = adSet.CampaignMetaID
	}
	if draft.Campaign.ExistingID != "" {
		campaign, err := p.parent(ctx, workspaceID, draft.Campaign.ExistingID, ads.LevelCampaign, "campaign.existingId")
		if err != nil {
			return err
		}
		parents.Campaign = campaign
	}
	for _, parent := range []*ads.Object{parents.Campaign, parents.AdSet} {
		if parent != nil && parent.AdAccountID != draft.AdAccountID {
			return ads.FieldError("adAccountId", "parent_in_other_account")
		}
	}
	draft.Adopt(parents)
	return nil
}

func (p preflighter) parent(ctx context.Context, workspaceID, metaID string, level ads.Level, field string) (*ads.Object, error) {
	o, err := p.objects.Find(ctx, workspaceID, metaID)
	switch {
	case errors.Is(err, ads.ErrObjectNotFound):
		return nil, ads.FieldError(field, "not_found")
	case err != nil:
		return nil, err
	case o.Level != level || o.Locked():
		return nil, ads.FieldError(field, "not_usable")
	}
	return o, nil
}

func (p preflighter) page(ctx context.Context, token string, account *ads.AdAccount, pageID string) (ads.RemotePage, error) {
	pages, err := p.gateway.ListPages(ctx, token)
	if err != nil {
		return ads.RemotePage{}, p.access.failed(ctx, account, err)
	}
	for _, page := range pages {
		if page.PageID == pageID {
			if !page.CanAdvertise {
				return ads.RemotePage{}, ads.FieldError("identity.pageId", "cannot_advertise")
			}
			return page, nil
		}
	}
	return ads.RemotePage{}, ads.FieldError("identity.pageId", "not_available")
}

func (p preflighter) checkIdentity(ctx context.Context, workspaceID string, draft ads.AdDraft, page ads.RemotePage) error {
	if id := draft.Identity.InstagramUserID; id != "" && id != page.InstagramUserID {
		return ads.FieldError("identity.instagramUserId", "not_linked_to_page")
	}
	if draft.NeedsLeadTerms() && !page.LeadTermsAccepted {
		return ads.FieldError("identity.pageId", "lead_terms_not_accepted")
	}
	if draft.AdSet.Destination != ads.DestinationWhatsApp || !draft.NewAdSet() {
		return nil
	}
	numbers, err := p.numbers.List(ctx, workspaceID)
	if err != nil {
		return err
	}
	return ads.WhatsAppDestination(page, numbers, draft.AdSet.WhatsAppNumber)
}

func (p preflighter) checkAudiences(ctx context.Context, token string, account *ads.AdAccount, draft ads.AdDraft) error {
	t := draft.AdSet.Targeting
	if !draft.NewAdSet() || len(t.CustomAudiences)+len(t.ExcludedCustomAudiences) == 0 {
		return nil
	}
	audiences, err := p.gateway.ListAudiences(ctx, token, account.MetaAccountID)
	if err != nil {
		return p.access.failed(ctx, account, err)
	}
	foreign := func(ref ads.TargetRef) bool {
		return !slices.ContainsFunc(audiences, func(a ads.Audience) bool { return a.MetaID == ref.ID })
	}
	if slices.ContainsFunc(t.CustomAudiences, foreign) {
		return ads.FieldError("adSet.targeting.customAudiences", "not_available")
	}
	if slices.ContainsFunc(t.ExcludedCustomAudiences, foreign) {
		return ads.FieldError("adSet.targeting.excludedCustomAudiences", "not_available")
	}
	return nil
}

func (p preflighter) checkPromotion(ctx context.Context, token string, account *ads.AdAccount, draft ads.AdDraft) error {
	if !draft.NewAdSet() {
		return nil
	}
	s := draft.AdSet
	if s.PixelID != "" && s.Goal.NeedsPixel() && s.Destination != ads.DestinationCatalog {
		pixels, err := p.gateway.ListPixels(ctx, token, account.MetaAccountID)
		if err != nil {
			return p.access.failed(ctx, account, err)
		}
		if !slices.ContainsFunc(pixels, func(px ads.Pixel) bool { return px.MetaID == s.PixelID && !px.Unavailable }) {
			return ads.FieldError("adSet.pixelId", "not_available")
		}
	}
	switch s.Destination {
	case ads.DestinationApp:
		apps, err := p.gateway.ListApps(ctx, token, account.MetaAccountID)
		if err != nil {
			return p.access.failed(ctx, account, err)
		}
		if !slices.ContainsFunc(apps, func(a ads.RemoteApp) bool { return a.ID == s.AppID && slices.Contains(a.StoreURLs, s.AppStoreURL) }) {
			return ads.FieldError("adSet.appId", "not_available")
		}
	case ads.DestinationCatalog:
		if account.BusinessID == "" {
			return ads.FieldError("adSet.catalogId", "no_business")
		}
		catalogs, err := p.gateway.ListCatalogs(ctx, token, account.BusinessID)
		if err != nil {
			return p.access.failed(ctx, account, err)
		}
		if !catalogHasSet(catalogs, s.CatalogID, s.ProductSetID) {
			return ads.FieldError("adSet.productSetId", "not_available")
		}
	}
	return nil
}

func catalogHasSet(catalogs []ads.RemoteCatalog, catalogID, setID string) bool {
	for _, c := range catalogs {
		if c.ID == catalogID {
			return slices.ContainsFunc(c.ProductSets, func(s ads.RemoteProductSet) bool { return s.ID == setID })
		}
	}
	return false
}

func (p preflighter) checkCreative(ctx context.Context, workspaceID, token string, draft ads.AdDraft, index int, c ads.CreativeDraft, mediaURLs map[string]string) error {
	prefix := "ads[" + strconv.Itoa(index) + "].creative."
	for _, ref := range c.MediaRefs() {
		url, err := p.media.describe(ctx, workspaceID, ref)
		if errors.Is(err, ads.ErrMediaNotImage) || errors.Is(err, media.ErrMediaNotFound) {
			return ads.FieldError(prefix+"media", "not_found")
		}
		if err != nil {
			return err
		}
		if url != "" {
			mediaURLs[ref.MediaID] = url
		}
	}
	pageID := draft.Identity.PageID
	if c.LeadFormID != "" && draft.AdSet.Destination == ads.DestinationInstantForm {
		forms, err := p.gateway.ListForms(ctx, token, pageID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(forms, func(f ads.LeadForm) bool { return f.MetaID == c.LeadFormID && f.Status == ads.FormActive }) {
			return ads.FieldError(prefix+"leadFormId", "not_available")
		}
	}
	switch c.Format {
	case ads.FormatExistingPost:
		posts, err := p.posts(ctx, token, draft, c)
		if err != nil {
			return err
		}
		want := c.PostID
		if want == "" {
			want = c.InstagramMediaID
		}
		if !slices.ContainsFunc(posts, func(post ads.RemotePost) bool { return post.ID == want }) {
			return ads.FieldError(prefix+"postId", "not_available")
		}
	case ads.FormatCollection:
		experiences, err := p.gateway.ListInstantExperiences(ctx, token, pageID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(experiences, func(e ads.RemoteInstantExperience) bool { return e.ID == c.InstantExperience }) {
			return ads.FieldError(prefix+"instantExperienceId", "not_available")
		}
	}
	return nil
}

func (p preflighter) posts(ctx context.Context, token string, draft ads.AdDraft, c ads.CreativeDraft) ([]ads.RemotePost, error) {
	if c.InstagramMediaID != "" {
		if draft.Identity.InstagramUserID == "" {
			return nil, ads.FieldError("identity.instagramUserId", "required")
		}
		return p.gateway.ListInstagramMedia(ctx, token, draft.Identity.InstagramUserID)
	}
	return p.gateway.ListPagePosts(ctx, token, draft.Identity.PageID)
}
