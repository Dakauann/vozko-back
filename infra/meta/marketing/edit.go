package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	metaMediaPrefix   = "meta:"
	roasFloorScale    = 10_000
	adSetDetailFields = adSetFields + ",bid_amount,bid_constraints,targeting,adset_schedule,promoted_object"
	adDetailFields    = "id,name,campaign_id,adset_id,status,effective_status,created_time,updated_time,issues_info,ad_review_feedback,adset{destination_type},creative_asset_groups_spec," +
		"creative{id,title,body,image_url,thumbnail_url,object_story_id,object_id,instagram_user_id,source_instagram_media_id,product_set_id,object_story_spec,asset_feed_spec,degrees_of_freedom_spec}"
)

var _ advertising.EditGateway = (*Gateway)(nil)

var detailFields = map[advertising.Level]string{
	advertising.LevelCampaign: campaignFields,
	advertising.LevelAdSet:    adSetDetailFields,
	advertising.LevelAd:       adDetailFields,
}

func (g *Gateway) GetObjectDetail(ctx context.Context, token, metaID string, level advertising.Level) (*advertising.ObjectDetail, error) {
	fields, ok := detailFields[level]
	if !ok {
		return nil, fmt.Errorf("marketing: unknown level %q", level)
	}
	path, err := objectPath(metaID)
	if err != nil {
		return nil, err
	}
	var row graphObject
	if err := g.get(ctx, path, token, fields, &row); err != nil {
		return nil, err
	}
	object, err := row.toDomain(level)
	if err != nil {
		return nil, err
	}
	detail := &advertising.ObjectDetail{Object: object, Budget: object.Budget()}
	switch level {
	case advertising.LevelCampaign:
		detail.Bid = advertising.Bid{Strategy: advertising.BidStrategy(row.BidStrategy)}
	case advertising.LevelAdSet:
		err = row.fillAdSetDetail(detail)
	case advertising.LevelAd:
		err = row.fillAdDetail(detail)
	}
	if err != nil {
		return nil, err
	}
	return detail, nil
}

func (o graphObject) fillAdSetDetail(detail *advertising.ObjectDetail) error {
	amount, err := o.BidAmount.minorUnits("bid_amount")
	if err != nil {
		return err
	}
	detail.Bid = advertising.Bid{Strategy: advertising.BidStrategy(o.BidStrategy), Amount: amount}
	if o.BidConstraints != nil {
		floor, err := o.BidConstraints.ROASAverageFloor.wholePart("roas_average_floor")
		if err != nil {
			return err
		}
		detail.Bid.ROASFloor = float64(floor) / roasFloorScale
	}
	if detail.Targeting, detail.Placements, err = targetingFrom(o.Targeting); err != nil {
		return err
	}
	for _, part := range o.AdSetSchedule {
		if part.TimezoneType != "" && part.TimezoneType != viewerTimezone {
			return fmt.Errorf("marketing: ad set %s schedules in %q time, which this editor cannot represent", o.ID, part.TimezoneType)
		}
		detail.Schedule = append(detail.Schedule, advertising.DayPart{Days: part.Days, StartMinute: part.StartMinute, EndMinute: part.EndMinute})
	}
	if o.PromotedObject != nil {
		detail.Identity.PageID = o.PromotedObject.PageID
	}
	return nil
}

func (o graphObject) fillAdDetail(detail *advertising.ObjectDetail) error {
	if o.AdSet != nil {
		detail.Object.DestinationType = o.AdSet.DestinationType
	}
	if o.Creative == nil {
		return fmt.Errorf("marketing: ad %s without creative", o.ID)
	}
	creative, err := o.Creative.draft(o.AssetGroups)
	if err != nil {
		return fmt.Errorf("marketing: ad %s: %w", o.ID, err)
	}
	detail.Creative = creative
	detail.Identity = o.Creative.identity()
	return nil
}

func (c graphCreative) identity() advertising.Identity {
	out := advertising.Identity{PageID: c.ObjectID.String(), InstagramUserID: c.InstagramUserID.String()}
	if story := c.ObjectStorySpec; story != nil {
		out.PageID = story.PageID
		if story.InstagramUserID != "" {
			out.InstagramUserID = story.InstagramUserID
		}
	}
	if out.PageID == "" && c.ObjectStoryID != "" {
		out.PageID, _, _ = strings.Cut(c.ObjectStoryID, "_")
	}
	return out
}

func mediaOf(kind advertising.MediaKind, metaID string) (advertising.MediaRef, error) {
	if metaID == "" {
		return advertising.MediaRef{}, fmt.Errorf("creative %s without meta id", kind)
	}
	return advertising.MediaRef{Kind: kind, MediaID: metaMediaPrefix + metaID}, nil
}

func realLink(link string) string {
	for _, placeholder := range messagingLinks {
		if link == placeholder {
			return ""
		}
	}
	if link == instantFormLink {
		return ""
	}
	return link
}

func (c graphCreative) enhanced() bool {
	if c.DegreesOfFreedomSpec == nil {
		return false
	}
	for _, feature := range c.DegreesOfFreedomSpec.CreativeFeaturesSpec {
		if feature.EnrollStatus == enrollStatusOn {
			return true
		}
	}
	return false
}

func applyCallToAction(d *advertising.CreativeDraft, cta *graphCallToAction) {
	if cta == nil {
		return
	}
	d.CallToAction = advertising.CallToAction(cta.Type)
	if cta.Value == nil {
		return
	}
	if d.Link == "" {
		d.Link = realLink(cta.Value.Link)
	}
	d.LeadFormID = cta.Value.LeadGenFormID
}

func applyWelcome(d *advertising.CreativeDraft, w *pageWelcome) {
	if w == nil {
		return
	}
	if w.Spec == nil {
		d.Greeting = w.Text
		return
	}
	message := w.Spec.TextFormat.Message
	for _, b := range message.IceBreakers {
		d.IceBreakers = append(d.IceBreakers, b.Title)
	}
	if len(d.IceBreakers) == 0 || message.Text != d.IceBreakers[0] {
		d.Greeting = message.Text
	}
}

func (c graphCreative) draft(groups *graphAssetGroups) (*advertising.CreativeDraft, error) {
	d := &advertising.CreativeDraft{Enhancements: c.enhanced()}
	story := c.ObjectStorySpec
	var err error
	switch {
	case groups != nil && len(groups.Groups) > 0:
		err = draftFromGroup(d, groups.Groups[0])
	case c.SourceInstagramMediaID != "":
		d.Format, d.InstagramMediaID = advertising.FormatExistingPost, c.SourceInstagramMediaID.String()
	case c.AssetFeedSpec != nil:
		err = draftFromFeed(d, *c.AssetFeedSpec)
	case story != nil && story.TemplateData != nil:
		d.Format = advertising.FormatCatalog
		draftFromLink(d, *story.TemplateData)
	case story != nil && story.VideoData != nil:
		v := story.VideoData
		d.Format, d.PrimaryText, d.Headline, d.Description = advertising.FormatVideo, v.Message, v.Title, v.LinkDescription
		applyCallToAction(d, v.CallToAction)
		applyWelcome(d, v.PageWelcomeMessage)
		d.Media, err = mediaOf(advertising.MediaVideo, v.VideoID)
	case story != nil && story.LinkData != nil:
		err = draftFromLinkData(d, *story.LinkData)
	case c.ObjectStoryID != "":
		d.Format, d.PostID = advertising.FormatExistingPost, c.ObjectStoryID
	default:
		return nil, fmt.Errorf("creative %s has a shape this editor cannot represent", c.ID)
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func draftFromLink(d *advertising.CreativeDraft, ld graphLinkData) {
	d.PrimaryText, d.Headline, d.Description, d.DisplayLink = ld.Message, ld.Name, ld.Description, ld.Caption
	d.Link = realLink(ld.Link)
	applyCallToAction(d, ld.CallToAction)
	applyWelcome(d, ld.PageWelcomeMessage)
}

func draftFromLinkData(d *advertising.CreativeDraft, ld graphLinkData) error {
	draftFromLink(d, ld)
	var err error
	switch {
	case len(ld.ChildAttachments) > 0:
		d.Format = advertising.FormatCarousel
		for _, child := range ld.ChildAttachments {
			card := advertising.CarouselCard{Headline: child.Name, Description: child.Description, Link: realLink(child.Link)}
			if child.VideoID != "" {
				card.Media, err = mediaOf(advertising.MediaVideo, child.VideoID)
			} else {
				card.Media, err = mediaOf(advertising.MediaImage, child.ImageHash)
			}
			if err != nil {
				return err
			}
			d.Cards = append(d.Cards, card)
		}
	case strings.HasPrefix(ld.Link, canvasLinkPrefix):
		d.Format, d.Link = advertising.FormatCollection, ""
		d.InstantExperience = strings.TrimPrefix(ld.Link, canvasLinkPrefix)
		d.Media, err = mediaOf(advertising.MediaImage, ld.ImageHash)
	default:
		d.Format = advertising.FormatImage
		d.Media, err = mediaOf(advertising.MediaImage, ld.ImageHash)
	}
	return err
}

func textValues(texts []graphText) []string {
	out := make([]string, 0, len(texts))
	for _, t := range texts {
		out = append(out, t.Text)
	}
	return out
}

func assetMediaRefs(images []graphAssetImage, videos []graphAssetVideo) ([]advertising.MediaRef, error) {
	var refs []advertising.MediaRef
	for _, img := range images {
		ref, err := mediaOf(advertising.MediaImage, img.Hash)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	for _, v := range videos {
		ref, err := mediaOf(advertising.MediaVideo, v.VideoID)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func draftFromFeed(d *advertising.CreativeDraft, feed graphAssetFeed) error {
	d.Format = advertising.FormatFlexible
	d.Texts, d.Headlines, d.Descriptions = textValues(feed.Bodies), textValues(feed.Titles), textValues(feed.Descriptions)
	if len(feed.LinkURLs) > 0 {
		d.Link, d.DisplayLink = realLink(feed.LinkURLs[0].WebsiteURL), feed.LinkURLs[0].DisplayURL
	}
	if len(feed.CallToActionTypes) > 0 {
		d.CallToAction = advertising.CallToAction(feed.CallToActionTypes[0])
	}
	var err error
	d.Medias, err = assetMediaRefs(feed.Images, feed.Videos)
	return err
}

func draftFromGroup(d *advertising.CreativeDraft, group graphAssetGroup) error {
	d.Format = advertising.FormatFlexible
	for _, t := range group.Texts {
		switch t.TextType {
		case textPrimary:
			d.Texts = append(d.Texts, t.Text)
		case textHeadline:
			d.Headlines = append(d.Headlines, t.Text)
		case textDescription:
			d.Descriptions = append(d.Descriptions, t.Text)
		default:
			return fmt.Errorf("asset group text type %q is unknown", t.TextType)
		}
	}
	applyCallToAction(d, group.CallToAction)
	var err error
	d.Medias, err = assetMediaRefs(group.Images, group.Videos)
	return err
}

func (g *Gateway) currentTargeting(ctx context.Context, token, path string) (map[string]json.RawMessage, error) {
	var out struct {
		Targeting map[string]json.RawMessage `json:"targeting"`
	}
	if err := g.get(ctx, path, token, "targeting", &out); err != nil {
		return nil, err
	}
	if out.Targeting == nil {
		return nil, fmt.Errorf("marketing: %s has no targeting to update", path)
	}
	return out.Targeting, nil
}

func onlyAt(field string, level advertising.Level, allowed ...advertising.Level) error {
	if !slices.Contains(allowed, level) {
		return fmt.Errorf("%w: %s on %s", advertising.ErrEditNotForLevel, field, level)
	}
	return nil
}

func editForm(level advertising.Level, spec advertising.EditSpec) (url.Values, error) {
	form := url.Values{}
	if spec.Name != nil {
		form.Set("name", *spec.Name)
	}
	if spec.Budget != nil {
		if err := onlyAt("budget", level, advertising.LevelCampaign, advertising.LevelAdSet); err != nil {
			return nil, err
		}
		if err := setBudget(form, *spec.Budget); err != nil {
			return nil, err
		}
	}
	if spec.Bid != nil {
		if err := onlyAt("bid", level, advertising.LevelCampaign, advertising.LevelAdSet); err != nil {
			return nil, err
		}
		set := setBid
		if level == advertising.LevelCampaign {
			set = setCampaignBid
		}
		if err := set(form, *spec.Bid); err != nil {
			return nil, err
		}
	}
	if spec.EndAt != nil {
		if err := onlyAt("end", level, advertising.LevelCampaign, advertising.LevelAdSet); err != nil {
			return nil, err
		}
		field := "end_time"
		if level == advertising.LevelCampaign {
			field = "stop_time"
		}
		form.Set(field, timeParam(*spec.EndAt))
	}
	if spec.Schedule != nil {
		if err := onlyAt("schedule", level, advertising.LevelAdSet); err != nil {
			return nil, err
		}
		if err := setSchedule(form, spec.Schedule); err != nil {
			return nil, err
		}
	}
	if spec.CreativeID != "" {
		if err := onlyAt("creative", level, advertising.LevelAd); err != nil {
			return nil, err
		}
		creative, err := jsonValue(map[string]string{"creative_id": spec.CreativeID})
		if err != nil {
			return nil, err
		}
		form.Set("creative", creative)
	}
	if spec.Targeting != nil || spec.Placements != nil {
		if err := onlyAt("targeting", level, advertising.LevelAdSet); err != nil {
			return nil, err
		}
	}
	return form, nil
}

func (g *Gateway) UpdateObject(ctx context.Context, token, metaID string, level advertising.Level, spec advertising.EditSpec) error {
	path, err := objectPath(metaID)
	if err != nil {
		return err
	}
	if !level.Valid() {
		return fmt.Errorf("marketing: unknown level %q", level)
	}
	form, err := editForm(level, spec)
	if err != nil {
		return err
	}
	if spec.Targeting != nil || spec.Placements != nil {
		current, err := g.currentTargeting(ctx, token, path)
		if err != nil {
			return err
		}
		merged, err := mergeTargeting(current, spec.Targeting, spec.Placements)
		if err != nil {
			return err
		}
		form.Set("targeting", merged)
	}
	if len(form) == 0 {
		return advertising.ErrNothingToChange
	}
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: token, Form: form, Idempotent: true})
}

func (g *Gateway) CopyObject(ctx context.Context, token, metaID string, level advertising.Level, req advertising.CopyRequest) (string, error) {
	path, err := objectPath(metaID)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("status_option", string(advertising.StatusPaused))
	deep := req.DeepCopy && level != advertising.LevelAd
	switch level {
	case advertising.LevelCampaign:
		if req.ParentID != "" {
			return "", fmt.Errorf("%w: parent on campaign", advertising.ErrEditNotForLevel)
		}
	case advertising.LevelAdSet:
		if req.ParentID != "" {
			form.Set("campaign_id", req.ParentID)
		}
	case advertising.LevelAd:
		if req.ParentID != "" {
			form.Set("adset_id", req.ParentID)
		}
	default:
		return "", fmt.Errorf("marketing: unknown level %q", level)
	}
	if level != advertising.LevelAd {
		form.Set("deep_copy", strconv.FormatBool(deep))
	}
	if req.NameSuffix != "" {
		strategy := "ONLY_TOP_LEVEL_RENAME"
		if deep {
			strategy = "DEEP_RENAME"
		}
		rename, err := jsonValue(map[string]string{"rename_strategy": strategy, "rename_suffix": req.NameSuffix})
		if err != nil {
			return "", err
		}
		form.Set("rename_options", rename)
	}
	var out map[string]json.RawMessage
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path + "/copies", Token: token, Form: form}, &out); err != nil {
		return "", err
	}
	key := "copied_" + string(level) + "_id"
	var copied meta.GraphID
	if err := json.Unmarshal(out[key], &copied); err != nil || copied == "" {
		return "", fmt.Errorf("marketing: copy of %s returned no %s", metaID, key)
	}
	return copied.String(), nil
}

func (g *Gateway) SetSpendCap(ctx context.Context, token, metaAccountID string, cap int64) error {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return err
	}
	if cap <= 0 {
		return fmt.Errorf("%w: spend cap must be positive", advertising.ErrInvalidBudget)
	}
	account, err := g.GetAdAccount(ctx, token, metaAccountID)
	if err != nil {
		return err
	}
	micros, err := advertising.MinorToMicros(account.Currency, cap)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("spend_cap", string(majorUnits(micros)))
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: token, Form: form, Idempotent: true})
}

func (g *Gateway) RemoveSpendCap(ctx context.Context, token, metaAccountID string) error {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("spend_cap_action", "delete")
	return g.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: path, Token: token, Form: form, Idempotent: true})
}
