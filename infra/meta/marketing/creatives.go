package marketing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const (
	instantFormLink   = "http://fb.me/"
	canvasLinkPrefix  = "https://fb.com/canvas_doc/"
	pageLinkPrefix    = "https://www.facebook.com/"
	collectionPhotos  = 4
	photoElement      = "PHOTO"
	enrollStatusOn    = "OPT_IN"
	enrollStatusOff   = "OPT_OUT"
	textPrimary       = "primary_text"
	textHeadline      = "headline"
	textDescription   = "description"
	formatSingleImage = "SINGLE_IMAGE"
	formatSingleVideo = "SINGLE_VIDEO"
	formatAutomatic   = "AUTOMATIC_FORMAT"
)

var messagingLinks = map[advertising.Destination]string{
	advertising.DestinationWhatsApp:        "https://api.whatsapp.com/send",
	advertising.DestinationMessenger:       "https://fb.com/messenger_doc/",
	advertising.DestinationInstagramDirect: "https://www.instagram.com/",
}

var enhancementFeatures = []string{
	"adapt_to_placement", "add_text_overlay", "creative_stickers", "description_automation", "enhance_cta",
	"image_animation", "image_background_gen", "image_brightness_and_contrast", "image_templates",
	"image_text_translation", "image_touchups", "image_uncrop", "inline_comment", "media_type_automation",
	"pac_relaxation", "product_extensions", "reveal_details_over_time", "text_optimizations", "text_translation",
	"translate_voiceover", "video_auto_crop", "video_filtering", "video_uncrop",
}

var approvedEnhancements = []string{"image_touchups", "text_optimizations", "enhance_cta", "adapt_to_placement"}

type graphCTAValue struct {
	AppDestination string `json:"app_destination,omitempty"`
	Link           string `json:"link,omitempty"`
	LeadGenFormID  string `json:"lead_gen_form_id,omitempty"`
}

type graphCallToAction struct {
	Type  string         `json:"type"`
	Value *graphCTAValue `json:"value,omitempty"`
}

type graphIceBreaker struct {
	Title string `json:"title"`
}

type graphAutofill struct {
	Content string `json:"content"`
}

type graphWelcomeText struct {
	Text            string            `json:"text"`
	IceBreakers     []graphIceBreaker `json:"ice_breakers,omitempty"`
	AutofillMessage *graphAutofill    `json:"autofill_message,omitempty"`
}

type graphWelcomeMessage struct {
	Type              string `json:"type"`
	Version           int    `json:"version"`
	LandingScreenType string `json:"landing_screen_type"`
	MediaType         string `json:"media_type"`
	TextFormat        struct {
		CustomerActionType string           `json:"customer_action_type"`
		Message            graphWelcomeText `json:"message"`
	} `json:"text_format"`
}

type graphCollectionThumbnail struct {
	ElementID    string              `json:"element_id"`
	ElementCrops map[string][][2]int `json:"element_crops"`
}

type graphLinkData struct {
	Link                 string                     `json:"link,omitempty"`
	Message              string                     `json:"message,omitempty"`
	Name                 string                     `json:"name,omitempty"`
	Description          string                     `json:"description,omitempty"`
	Caption              string                     `json:"caption,omitempty"`
	ImageHash            string                     `json:"image_hash,omitempty"`
	VideoID              string                     `json:"video_id,omitempty"`
	Picture              string                     `json:"picture,omitempty"`
	CallToAction         *graphCallToAction         `json:"call_to_action,omitempty"`
	PageWelcomeMessage   *graphWelcomeMessage       `json:"page_welcome_message,omitempty"`
	ChildAttachments     []graphLinkData            `json:"child_attachments,omitempty"`
	MultiShareOptimized  *bool                      `json:"multi_share_optimized,omitempty"`
	MultiShareEndCard    *bool                      `json:"multi_share_end_card,omitempty"`
	CollectionThumbnails []graphCollectionThumbnail `json:"collection_thumbnails,omitempty"`
}

type graphVideoData struct {
	VideoID            string               `json:"video_id"`
	ImageURL           string               `json:"image_url,omitempty"`
	ImageHash          string               `json:"image_hash,omitempty"`
	Message            string               `json:"message,omitempty"`
	Title              string               `json:"title,omitempty"`
	LinkDescription    string               `json:"link_description,omitempty"`
	CallToAction       *graphCallToAction   `json:"call_to_action,omitempty"`
	PageWelcomeMessage *graphWelcomeMessage `json:"page_welcome_message,omitempty"`
}

type graphObjectStorySpec struct {
	PageID          string          `json:"page_id"`
	InstagramUserID string          `json:"instagram_user_id,omitempty"`
	LinkData        *graphLinkData  `json:"link_data,omitempty"`
	VideoData       *graphVideoData `json:"video_data,omitempty"`
	TemplateData    *graphLinkData  `json:"template_data,omitempty"`
}

type graphText struct {
	Text string `json:"text"`
}

type graphAssetImage struct {
	Hash string `json:"hash"`
}

type graphAssetVideo struct {
	VideoID      string `json:"video_id"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

type graphLinkURL struct {
	WebsiteURL string `json:"website_url"`
	DisplayURL string `json:"display_url,omitempty"`
}

type graphAssetFeed struct {
	Images            []graphAssetImage `json:"images,omitempty"`
	Videos            []graphAssetVideo `json:"videos,omitempty"`
	Bodies            []graphText       `json:"bodies,omitempty"`
	Titles            []graphText       `json:"titles,omitempty"`
	Descriptions      []graphText       `json:"descriptions,omitempty"`
	LinkURLs          []graphLinkURL    `json:"link_urls,omitempty"`
	CallToActionTypes []string          `json:"call_to_action_types,omitempty"`
	AdFormats         []string          `json:"ad_formats,omitempty"`
}

type graphGroupText struct {
	Text     string `json:"text"`
	TextType string `json:"text_type"`
}

type graphAssetGroup struct {
	Images       []graphAssetImage  `json:"images,omitempty"`
	Videos       []graphAssetVideo  `json:"videos,omitempty"`
	Texts        []graphGroupText   `json:"texts,omitempty"`
	CallToAction *graphCallToAction `json:"call_to_action,omitempty"`
}

type graphAssetGroups struct {
	Groups []graphAssetGroup `json:"groups"`
}

type enrollment struct {
	EnrollStatus string `json:"enroll_status"`
}

func enhancementsOf(on bool) map[string]map[string]enrollment {
	features := make(map[string]enrollment, len(enhancementFeatures))
	for _, f := range enhancementFeatures {
		status := enrollStatusOff
		if on && slices.Contains(approvedEnhancements, f) {
			status = enrollStatusOn
		}
		features[f] = enrollment{EnrollStatus: status}
	}
	return map[string]map[string]enrollment{"creative_features_spec": features}
}

func welcomeMessageOf(greeting string, iceBreakers []string) *graphWelcomeMessage {
	if greeting == "" && len(iceBreakers) == 0 {
		return nil
	}
	out := &graphWelcomeMessage{Type: "VISUAL_EDITOR", Version: 2, LandingScreenType: "welcome_message", MediaType: "text"}
	if len(iceBreakers) == 0 {
		out.TextFormat.CustomerActionType = "autofill_message"
		out.TextFormat.Message = graphWelcomeText{Text: greeting, AutofillMessage: &graphAutofill{Content: greeting}}
		return out
	}
	text := greeting
	if text == "" {
		text = iceBreakers[0]
	}
	breakers := make([]graphIceBreaker, 0, len(iceBreakers))
	for _, b := range iceBreakers {
		breakers = append(breakers, graphIceBreaker{Title: b})
	}
	out.TextFormat.CustomerActionType = "ice_breakers"
	out.TextFormat.Message = graphWelcomeText{Text: text, IceBreakers: breakers}
	return out
}

type uploadedRefs struct {
	images map[string]string
	videos map[string]string
}

func uploadedRefsOf(spec advertising.CreativeSpec) (uploadedRefs, error) {
	out := uploadedRefs{images: map[string]string{}, videos: map[string]string{}}
	for _, ref := range spec.Creative.MediaRefs() {
		switch ref.Kind {
		case advertising.MediaImage:
			hash := spec.Media.ImageHash(ref.MediaID)
			if hash == "" {
				return uploadedRefs{}, fmt.Errorf("marketing: image %s was not uploaded to meta", ref.MediaID)
			}
			out.images[ref.MediaID] = hash
		case advertising.MediaVideo:
			id := spec.Media.VideoID(ref.MediaID)
			if id == "" {
				return uploadedRefs{}, fmt.Errorf("marketing: video %s was not uploaded to meta", ref.MediaID)
			}
			out.videos[ref.MediaID] = id
		default:
			return uploadedRefs{}, fmt.Errorf("marketing: media %s has unknown kind %q", ref.MediaID, ref.Kind)
		}
	}
	return out, nil
}

func (u uploadedRefs) videoIDs() []string {
	ids := make([]string, 0, len(u.videos))
	for _, id := range u.videos {
		ids = append(ids, id)
	}
	return ids
}

func (u uploadedRefs) image(ref advertising.MediaRef) (string, error) {
	hash, ok := u.images[ref.MediaID]
	if ref.Kind != advertising.MediaImage || !ok {
		return "", fmt.Errorf("marketing: media %q must be an uploaded image", ref.MediaID)
	}
	return hash, nil
}

func (u uploadedRefs) video(ref advertising.MediaRef) (string, error) {
	id, ok := u.videos[ref.MediaID]
	if ref.Kind != advertising.MediaVideo || !ok {
		return "", fmt.Errorf("marketing: media %q must be an uploaded video", ref.MediaID)
	}
	return id, nil
}

func linkOf(spec advertising.CreativeSpec) (string, error) {
	d := spec.Destination
	if link, ok := messagingLinks[d]; ok {
		return link, nil
	}
	switch d {
	case advertising.DestinationInstantForm:
		if spec.Creative.Link != "" {
			return spec.Creative.Link, nil
		}
		return instantFormLink, nil
	case advertising.DestinationApp:
		if spec.AppStoreURL == "" {
			return "", fmt.Errorf("marketing: app ads need the app store url")
		}
		return spec.AppStoreURL, nil
	case advertising.DestinationWebsite, advertising.DestinationCatalog:
		if spec.Creative.Link == "" {
			return "", fmt.Errorf("marketing: %s ads need a link", d)
		}
		return spec.Creative.Link, nil
	case advertising.DestinationNone, advertising.DestinationOnPost:
		if spec.Creative.Link != "" {
			return spec.Creative.Link, nil
		}
		return pageLinkPrefix + spec.Identity.PageID, nil
	}
	return "", fmt.Errorf("marketing: unknown destination %q", d)
}

func callToActionOf(spec advertising.CreativeSpec, link string) (*graphCallToAction, error) {
	if spec.CallToAction == "" {
		return nil, fmt.Errorf("marketing: call to action is required")
	}
	out := &graphCallToAction{Type: string(spec.CallToAction)}
	switch {
	case spec.Destination.Messaging():
		out.Value = &graphCTAValue{AppDestination: string(spec.Destination)}
	case spec.CallToAction == advertising.CTANoButton:
	case spec.Destination == advertising.DestinationInstantForm:
		if spec.Creative.LeadFormID == "" {
			return nil, fmt.Errorf("marketing: instant form ads need a lead form")
		}
		out.Value = &graphCTAValue{Link: link, LeadGenFormID: spec.Creative.LeadFormID}
	default:
		out.Value = &graphCTAValue{Link: link}
	}
	return out, nil
}

func linkAndCallToAction(spec advertising.CreativeSpec) (string, *graphCallToAction, error) {
	link, err := linkOf(spec)
	if err != nil {
		return "", nil, err
	}
	cta, err := callToActionOf(spec, link)
	if err != nil {
		return "", nil, err
	}
	return link, cta, nil
}

func welcomeOf(spec advertising.CreativeSpec) *graphWelcomeMessage {
	if !spec.Destination.Messaging() {
		return nil
	}
	return welcomeMessageOf(spec.Creative.Greeting, spec.Creative.IceBreakers)
}

func checkAssetMode(spec advertising.CreativeSpec) error {
	flexible := spec.Creative.Format == advertising.FormatFlexible
	switch spec.AssetMode {
	case advertising.AssetsSingle:
		if flexible {
			return fmt.Errorf("marketing: flexible creatives need an asset mode")
		}
	case advertising.AssetsDynamic, advertising.AssetsGroups:
		if !flexible {
			return fmt.Errorf("marketing: asset mode %q is only for flexible creatives", spec.AssetMode)
		}
	default:
		return fmt.Errorf("marketing: unknown asset mode %q", spec.AssetMode)
	}
	return nil
}

type creativeRequest struct {
	g     *Gateway
	token string
	spec  advertising.CreativeSpec
	media uploadedRefs
	form  url.Values
}

func (g *Gateway) CreateCreative(ctx context.Context, token, metaAccountID string, spec advertising.CreativeSpec) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	if err := checkAssetMode(spec); err != nil {
		return "", err
	}
	if strings.TrimSpace(spec.Identity.PageID) == "" {
		return "", fmt.Errorf("marketing: creative needs a page")
	}
	media, err := uploadedRefsOf(spec)
	if err != nil {
		return "", err
	}
	enhancements, err := jsonValue(enhancementsOf(spec.Creative.Enhancements))
	if err != nil {
		return "", err
	}
	r := creativeRequest{g: g, token: token, spec: spec, media: media, form: url.Values{}}
	r.form.Set("name", spec.Name)
	r.form.Set("degrees_of_freedom_spec", enhancements)
	if err := r.build(ctx); err != nil {
		return "", err
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/adcreatives", Token: token, Form: r.form}, "ad creative")
}

func (r *creativeRequest) build(ctx context.Context) error {
	switch r.spec.Creative.Format {
	case advertising.FormatImage:
		return r.image()
	case advertising.FormatVideo:
		return r.video(ctx)
	case advertising.FormatCarousel:
		return r.carousel(ctx)
	case advertising.FormatExistingPost:
		return r.existingPost()
	case advertising.FormatCollection:
		return r.collection(ctx)
	case advertising.FormatCatalog:
		return r.catalog()
	case advertising.FormatFlexible:
		if r.spec.AssetMode == advertising.AssetsGroups {
			return r.story(r.identity())
		}
		return r.dynamic(ctx)
	}
	return fmt.Errorf("marketing: unknown creative format %q", r.spec.Creative.Format)
}

func (r *creativeRequest) identity() graphObjectStorySpec {
	return graphObjectStorySpec{PageID: r.spec.Identity.PageID, InstagramUserID: r.spec.Identity.InstagramUserID}
}

func (r *creativeRequest) story(spec graphObjectStorySpec) error {
	encoded, err := jsonValue(spec)
	if err != nil {
		return err
	}
	r.form.Set("object_story_spec", encoded)
	return nil
}

func (r *creativeRequest) image() error {
	c := r.spec.Creative
	hash, err := r.media.image(c.Media)
	if err != nil {
		return err
	}
	link, cta, err := linkAndCallToAction(r.spec)
	if err != nil {
		return err
	}
	story := r.identity()
	story.LinkData = &graphLinkData{
		Link: link, Message: c.PrimaryText, Name: c.Headline, Description: c.Description, Caption: c.DisplayLink,
		ImageHash: hash, CallToAction: cta, PageWelcomeMessage: welcomeOf(r.spec),
	}
	return r.story(story)
}

func (r *creativeRequest) thumbnails(ctx context.Context, videoIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(videoIDs))
	for _, id := range videoIDs {
		if _, done := out[id]; done {
			continue
		}
		video, err := r.g.VideoStatus(ctx, r.token, id)
		if err != nil {
			return nil, err
		}
		if video.State != advertising.VideoReady {
			return nil, fmt.Errorf("%w: video %s is %s", advertising.ErrVideoNotReady, id, video.State)
		}
		if video.ThumbnailURL == "" {
			return nil, fmt.Errorf("marketing: video %s has no preferred thumbnail", id)
		}
		out[id] = video.ThumbnailURL
	}
	return out, nil
}

func (r *creativeRequest) video(ctx context.Context) error {
	c := r.spec.Creative
	videoID, err := r.media.video(c.Media)
	if err != nil {
		return err
	}
	_, cta, err := linkAndCallToAction(r.spec)
	if err != nil {
		return err
	}
	thumbs, err := r.thumbnails(ctx, []string{videoID})
	if err != nil {
		return err
	}
	story := r.identity()
	story.VideoData = &graphVideoData{
		VideoID: videoID, ImageURL: thumbs[videoID], Message: c.PrimaryText, Title: c.Headline,
		LinkDescription: c.Description, CallToAction: cta, PageWelcomeMessage: welcomeOf(r.spec),
	}
	return r.story(story)
}

func (r *creativeRequest) carousel(ctx context.Context) error {
	c := r.spec.Creative
	top := r.spec
	if top.Creative.Link == "" && len(c.Cards) > 0 {
		top.Creative.Link = c.Cards[0].Link
	}
	link, cta, err := linkAndCallToAction(top)
	if err != nil {
		return err
	}
	cards := make([]graphLinkData, 0, len(c.Cards))
	for _, card := range c.Cards {
		cardSpec := top
		if card.Link != "" && !r.spec.Destination.Messaging() {
			cardSpec.Creative.Link = card.Link
		}
		cardLink, cardCTA, err := linkAndCallToAction(cardSpec)
		if err != nil {
			return err
		}
		child := graphLinkData{Link: cardLink, Name: card.Headline, Description: card.Description, CallToAction: cardCTA}
		if card.Media.Kind == advertising.MediaVideo {
			child.VideoID, err = r.media.video(card.Media)
		} else {
			child.ImageHash, err = r.media.image(card.Media)
		}
		if err != nil {
			return err
		}
		cards = append(cards, child)
	}
	thumbs, err := r.thumbnails(ctx, r.media.videoIDs())
	if err != nil {
		return err
	}
	for i := range cards {
		if cards[i].VideoID != "" {
			cards[i].Picture = thumbs[cards[i].VideoID]
		}
	}
	optimized, endCard := true, false
	story := r.identity()
	story.LinkData = &graphLinkData{
		Link: link, Message: c.PrimaryText, Caption: c.DisplayLink, CallToAction: cta, PageWelcomeMessage: welcomeOf(r.spec),
		ChildAttachments: cards, MultiShareOptimized: &optimized, MultiShareEndCard: &endCard,
	}
	return r.story(story)
}

func (r *creativeRequest) existingPost() error {
	c := r.spec.Creative
	if c.PostID != "" {
		r.form.Set("object_story_id", c.PostID)
		return nil
	}
	if r.spec.Identity.InstagramUserID == "" {
		return fmt.Errorf("marketing: instagram posts need the instagram account")
	}
	r.form.Set("object_id", r.spec.Identity.PageID)
	r.form.Set("instagram_user_id", r.spec.Identity.InstagramUserID)
	r.form.Set("source_instagram_media_id", c.InstagramMediaID)
	if r.spec.Destination == advertising.DestinationOnPost || r.spec.Destination == advertising.DestinationNone {
		return nil
	}
	_, cta, err := linkAndCallToAction(r.spec)
	if err != nil {
		return err
	}
	encoded, err := jsonValue(cta)
	if err != nil {
		return err
	}
	r.form.Set("call_to_action", encoded)
	return nil
}

type graphCanvas struct {
	BodyElements graphList[struct {
		ID          meta.GraphID `json:"id"`
		ElementType string       `json:"element_type"`
	}] `json:"body_elements"`
}

func (r *creativeRequest) collection(ctx context.Context) error {
	c := r.spec.Creative
	canvasID := strings.TrimSpace(c.InstantExperience)
	path, err := objectPath(canvasID)
	if err != nil {
		return err
	}
	if c.Media.Kind != advertising.MediaImage {
		return fmt.Errorf("marketing: collection ads with a video cover are not supported yet")
	}
	hash, err := r.media.image(c.Media)
	if err != nil {
		return err
	}
	spec := r.spec
	spec.Creative.Link = canvasLinkPrefix + canvasID
	link, cta, err := linkAndCallToAction(spec)
	if err != nil {
		return err
	}
	var canvas graphCanvas
	if err := r.g.get(ctx, path, r.token, "body_elements", &canvas); err != nil {
		return err
	}
	var thumbnails []graphCollectionThumbnail
	for _, element := range canvas.BodyElements {
		if element.ElementType != photoElement || len(thumbnails) == collectionPhotos {
			continue
		}
		if element.ID == "" {
			return fmt.Errorf("marketing: instant experience %s has a photo without id", canvasID)
		}
		thumbnails = append(thumbnails, graphCollectionThumbnail{
			ElementID:    element.ID.String(),
			ElementCrops: map[string][][2]int{"100x100": {{0, 0}, {100, 100}}},
		})
	}
	if len(thumbnails) < collectionPhotos {
		return fmt.Errorf("marketing: instant experience %s has %d photos, collection ads need %d", canvasID, len(thumbnails), collectionPhotos)
	}
	story := r.identity()
	story.LinkData = &graphLinkData{
		Link: link, Message: c.PrimaryText, Name: c.Headline, Description: c.Description,
		ImageHash: hash, CallToAction: cta, CollectionThumbnails: thumbnails,
	}
	return r.story(story)
}

func (r *creativeRequest) catalog() error {
	if r.spec.ProductSetID == "" {
		return fmt.Errorf("marketing: catalog ads need a product set")
	}
	link, err := linkOf(r.spec)
	if err != nil {
		return err
	}
	if r.spec.CallToAction == "" {
		return fmt.Errorf("marketing: call to action is required")
	}
	c := r.spec.Creative
	endCard := true
	story := r.identity()
	story.TemplateData = &graphLinkData{
		Link: link, Message: c.PrimaryText, Name: c.Headline, Description: c.Description,
		CallToAction: &graphCallToAction{Type: string(r.spec.CallToAction)}, MultiShareEndCard: &endCard,
	}
	r.form.Set("product_set_id", r.spec.ProductSetID)
	return r.story(story)
}

func textsOf(values []string) []graphText {
	out := make([]graphText, 0, len(values))
	for _, v := range values {
		out = append(out, graphText{Text: v})
	}
	return out
}

func (r *creativeRequest) dynamic(ctx context.Context) error {
	if r.spec.Destination == advertising.DestinationInstantForm {
		return fmt.Errorf("marketing: dynamic creative with an instant form is not supported yet")
	}
	c := r.spec.Creative
	link, _, err := linkAndCallToAction(r.spec)
	if err != nil {
		return err
	}
	feed := graphAssetFeed{
		Bodies:            textsOf(c.Texts),
		Titles:            textsOf(c.Headlines),
		Descriptions:      textsOf(c.Descriptions),
		LinkURLs:          []graphLinkURL{{WebsiteURL: link, DisplayURL: c.DisplayLink}},
		CallToActionTypes: []string{string(r.spec.CallToAction)},
	}
	images, videos := assetMediaOf(c.Medias, r.media)
	thumbs, err := r.thumbnails(ctx, r.media.videoIDs())
	if err != nil {
		return err
	}
	for i := range videos {
		videos[i].ThumbnailURL = thumbs[videos[i].VideoID]
	}
	feed.Images, feed.Videos = images, videos
	switch {
	case len(images) > 0 && len(videos) > 0:
		feed.AdFormats = []string{formatAutomatic}
	case len(videos) > 0:
		feed.AdFormats = []string{formatSingleVideo}
	default:
		feed.AdFormats = []string{formatSingleImage}
	}
	encoded, err := jsonValue(feed)
	if err != nil {
		return err
	}
	r.form.Set("asset_feed_spec", encoded)
	return r.story(r.identity())
}

func assetMediaOf(refs []advertising.MediaRef, media uploadedRefs) ([]graphAssetImage, []graphAssetVideo) {
	var images []graphAssetImage
	var videos []graphAssetVideo
	for _, ref := range refs {
		if ref.Kind == advertising.MediaVideo {
			videos = append(videos, graphAssetVideo{VideoID: media.videos[ref.MediaID]})
			continue
		}
		images = append(images, graphAssetImage{Hash: media.images[ref.MediaID]})
	}
	return images, videos
}

func groupTexts(values []string, kind string) []graphGroupText {
	out := make([]graphGroupText, 0, len(values))
	for _, v := range values {
		out = append(out, graphGroupText{Text: v, TextType: kind})
	}
	return out
}

func assetGroupsOf(spec advertising.CreativeSpec) (graphAssetGroups, error) {
	if spec.AssetMode != advertising.AssetsGroups || spec.Creative.Format != advertising.FormatFlexible {
		return graphAssetGroups{}, fmt.Errorf("marketing: asset groups need a flexible creative in groups mode")
	}
	media, err := uploadedRefsOf(spec)
	if err != nil {
		return graphAssetGroups{}, err
	}
	images, videos := assetMediaOf(spec.Creative.Medias, media)
	if len(images) == 0 && len(videos) == 0 {
		return graphAssetGroups{}, fmt.Errorf("marketing: an asset group needs an image or a video")
	}
	_, cta, err := linkAndCallToAction(spec)
	if err != nil {
		return graphAssetGroups{}, err
	}
	c := spec.Creative
	texts := groupTexts(c.Texts, textPrimary)
	texts = append(texts, groupTexts(c.Headlines, textHeadline)...)
	texts = append(texts, groupTexts(c.Descriptions, textDescription)...)
	group := graphAssetGroup{Images: images, Videos: videos, Texts: texts, CallToAction: cta}
	return graphAssetGroups{Groups: []graphAssetGroup{group}}, nil
}
