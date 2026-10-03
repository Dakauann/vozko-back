package advertising

import (
	"slices"
	"strings"
	"unicode/utf8"
)

type CreativeFormat string

const (
	FormatImage        CreativeFormat = "IMAGE"
	FormatVideo        CreativeFormat = "VIDEO"
	FormatCarousel     CreativeFormat = "CAROUSEL"
	FormatExistingPost CreativeFormat = "EXISTING_POST"
	FormatFlexible     CreativeFormat = "FLEXIBLE"
	FormatCatalog      CreativeFormat = "CATALOG"
	FormatCollection   CreativeFormat = "COLLECTION"
)

var creativeFormats = []CreativeFormat{FormatImage, FormatVideo, FormatCarousel, FormatExistingPost, FormatFlexible, FormatCatalog, FormatCollection}

func CreativeFormats() []CreativeFormat { return append([]CreativeFormat(nil), creativeFormats...) }

type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
)

type MediaRef struct {
	Kind    MediaKind `json:"kind"`
	MediaID string    `json:"mediaId"`
}

type CarouselCard struct {
	Media       MediaRef `json:"media"`
	Headline    string   `json:"headline,omitempty"`
	Description string   `json:"description,omitempty"`
	Link        string   `json:"link,omitempty"`
}

type CallToAction string

const (
	CTALearnMore       CallToAction = "LEARN_MORE"
	CTAShopNow         CallToAction = "SHOP_NOW"
	CTASignUp          CallToAction = "SIGN_UP"
	CTAContactUs       CallToAction = "CONTACT_US"
	CTADownload        CallToAction = "DOWNLOAD"
	CTABookNow         CallToAction = "BOOK_NOW"
	CTAGetOffer        CallToAction = "GET_OFFER"
	CTASubscribe       CallToAction = "SUBSCRIBE"
	CTAApplyNow        CallToAction = "APPLY_NOW"
	CTAOrderNow        CallToAction = "ORDER_NOW"
	CTAGetQuote        CallToAction = "GET_QUOTE"
	CTAInstallApp      CallToAction = "INSTALL_MOBILE_APP"
	CTAWatchMore       CallToAction = "WATCH_MORE"
	CTAWhatsAppMessage CallToAction = "WHATSAPP_MESSAGE"
	CTAMessagePage     CallToAction = "MESSAGE_PAGE"
	CTAInstagramDirect CallToAction = "INSTAGRAM_MESSAGE"
	CTANoButton        CallToAction = "NO_BUTTON"
)

var linkCallsToAction = []CallToAction{CTALearnMore, CTAShopNow, CTASignUp, CTAContactUs, CTADownload, CTABookNow, CTAGetOffer, CTASubscribe, CTAApplyNow, CTAOrderNow, CTAGetQuote, CTAWatchMore, CTANoButton}

func LinkCallsToAction() []CallToAction { return append([]CallToAction(nil), linkCallsToAction...) }

func CallsToActionFor(d Destination) []CallToAction {
	if cta := MessagingCallToAction(d); cta != "" {
		return []CallToAction{cta}
	}
	if d == DestinationApp {
		return append([]CallToAction{CTAInstallApp}, linkCallsToAction...)
	}
	return LinkCallsToAction()
}

func ValidPostPlatform(platform string) bool {
	return platform == PlatformFacebook || platform == PlatformInstagram
}

func MessagingCallToAction(d Destination) CallToAction {
	switch d {
	case DestinationWhatsApp:
		return CTAWhatsAppMessage
	case DestinationMessenger:
		return CTAMessagePage
	case DestinationInstagramDirect:
		return CTAInstagramDirect
	}
	return ""
}

type CreativeDraft struct {
	Format            CreativeFormat `json:"format"`
	PrimaryText       string         `json:"primaryText,omitempty"`
	Headline          string         `json:"headline,omitempty"`
	Description       string         `json:"description,omitempty"`
	Media             MediaRef       `json:"media,omitempty"`
	Cards             []CarouselCard `json:"cards,omitempty"`
	Texts             []string       `json:"texts,omitempty"`
	Headlines         []string       `json:"headlines,omitempty"`
	Descriptions      []string       `json:"descriptions,omitempty"`
	Medias            []MediaRef     `json:"medias,omitempty"`
	PostID            string         `json:"postId,omitempty"`
	InstagramMediaID  string         `json:"instagramMediaId,omitempty"`
	InstantExperience string         `json:"instantExperienceId,omitempty"`
	Link              string         `json:"link,omitempty"`
	DisplayLink       string         `json:"displayLink,omitempty"`
	CallToAction      CallToAction   `json:"callToAction,omitempty"`
	LeadFormID        string         `json:"leadFormId,omitempty"`
	Greeting          string         `json:"greeting,omitempty"`
	IceBreakers       []string       `json:"iceBreakers,omitempty"`
	Enhancements      bool           `json:"enhancements,omitempty"`
}

const (
	maxPrimaryTextRunes = 2200
	maxHeadlineRunes    = 255
	maxGreetingRunes    = 300
	maxIceBreakers      = 3
	maxIceBreakerRunes  = 80
	minCarouselCards    = 2
	maxCarouselCards    = 10
	maxFlexibleTexts    = 5
	maxFlexibleMedias   = 10
)

func (c *CreativeDraft) Normalize() {
	if c.Format == "" {
		c.Format = FormatImage
	}
	c.PrimaryText = strings.TrimSpace(c.PrimaryText)
	c.Headline = strings.TrimSpace(c.Headline)
	c.Description = strings.TrimSpace(c.Description)
	c.Link = strings.TrimSpace(c.Link)
	c.Greeting = strings.TrimSpace(c.Greeting)
	c.IceBreakers = nonBlank(c.IceBreakers)
	c.Texts = nonBlank(c.Texts)
	c.Headlines = nonBlank(c.Headlines)
	c.Descriptions = nonBlank(c.Descriptions)
}

func nonBlank(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (c CreativeDraft) MediaRefs() []MediaRef {
	var refs []MediaRef
	add := func(m MediaRef) {
		if m.MediaID != "" && !slices.Contains(refs, m) {
			refs = append(refs, m)
		}
	}
	add(c.Media)
	for _, card := range c.Cards {
		add(card.Media)
	}
	for _, m := range c.Medias {
		add(m)
	}
	return refs
}

func formatsFor(d Destination) []CreativeFormat {
	switch d {
	case DestinationOnPost:
		return []CreativeFormat{FormatExistingPost}
	case DestinationCatalog:
		return []CreativeFormat{FormatCatalog, FormatCollection}
	case DestinationInstantForm, DestinationWebsite, DestinationApp, DestinationNone:
		return []CreativeFormat{FormatImage, FormatVideo, FormatCarousel, FormatFlexible, FormatExistingPost}
	}
	return []CreativeFormat{FormatImage, FormatVideo, FormatCarousel, FormatFlexible}
}

func (f CreativeFormat) SingleMediaKind() (MediaKind, bool) {
	switch f {
	case FormatImage:
		return MediaImage, true
	case FormatVideo:
		return MediaVideo, true
	}
	return "", false
}

func (c CreativeDraft) validate(v issues, d Destination) {
	if !slices.Contains(formatsFor(d), c.Format) {
		v.add("format", "not_for_destination")
		return
	}
	if c.Format != FormatExistingPost && c.Format != FormatFlexible {
		v.text("primaryText", c.PrimaryText, true, maxPrimaryTextRunes)
	}
	v.text("headline", c.Headline, false, maxHeadlineRunes)
	v.text("description", c.Description, false, maxHeadlineRunes)
	if kind, single := c.Format.SingleMediaKind(); single {
		checkMedia(v.at("media"), c.Media, kind)
	}
	switch c.Format {
	case FormatCarousel:
		c.validateCarousel(v, d)
	case FormatFlexible:
		c.validateFlexible(v)
	case FormatExistingPost:
		if (c.PostID == "") == (c.InstagramMediaID == "") {
			v.add("postId", "required")
		}
	case FormatCatalog:
	case FormatCollection:
		if strings.TrimSpace(c.InstantExperience) == "" {
			v.add("instantExperienceId", "required")
		}
		checkMedia(v.at("media"), c.Media, c.Media.Kind)
	}
	c.validateDestination(v, d)
}

func checkMedia(v issues, m MediaRef, want MediaKind) {
	if strings.TrimSpace(m.MediaID) == "" {
		v.add("", "required")
		return
	}
	if m.Kind != want || (want != MediaImage && want != MediaVideo) {
		v.add("", "wrong_kind")
	}
}

func (c CreativeDraft) validateCarousel(v issues, d Destination) {
	if len(c.Cards) < minCarouselCards || len(c.Cards) > maxCarouselCards {
		v.add("cards", "count")
	}
	for i, card := range c.Cards {
		cv := v.item("cards", i)
		checkMedia(cv.at("media"), card.Media, card.Media.Kind)
		cv.text("headline", card.Headline, false, maxHeadlineRunes)
		if d == DestinationWebsite {
			cv.url("link", card.Link, false)
		}
	}
}

func (c CreativeDraft) validateFlexible(v issues) {
	if len(c.Texts) == 0 || len(c.Texts) > maxFlexibleTexts {
		v.add("texts", "count")
	}
	if len(c.Headlines) > maxFlexibleTexts || len(c.Descriptions) > maxFlexibleTexts {
		v.add("headlines", "count")
	}
	if len(c.Medias) == 0 || len(c.Medias) > maxFlexibleMedias {
		v.add("medias", "count")
	}
	for i, m := range c.Medias {
		checkMedia(v.item("medias", i), m, m.Kind)
	}
	for i, t := range c.Texts {
		if utf8.RuneCountInString(t) > maxPrimaryTextRunes {
			v.item("texts", i).add("", "too_long")
		}
	}
}

func (c CreativeDraft) validateDestination(v issues, d Destination) {
	switch {
	case d.Messaging():
		v.text("greeting", c.Greeting, false, maxGreetingRunes)
		if len(c.IceBreakers) > maxIceBreakers {
			v.add("iceBreakers", "too_many")
		}
		if slices.ContainsFunc(c.IceBreakers, func(b string) bool { return utf8.RuneCountInString(b) > maxIceBreakerRunes }) {
			v.add("iceBreakers", "too_long")
		}
	case d == DestinationWebsite || d == DestinationCatalog:
		if c.Format != FormatExistingPost && c.Format != FormatCarousel {
			v.url("link", c.Link, true)
		}
		if c.Format == FormatCarousel {
			v.url("link", c.Link, false)
		}
		c.checkLinkCTA(v, d)
	case d == DestinationApp:
		c.checkLinkCTA(v, d)
	case d == DestinationInstantForm:
		if strings.TrimSpace(c.LeadFormID) == "" {
			v.add("leadFormId", "required")
		}
		c.checkLinkCTA(v, d)
	}
}

func (c CreativeDraft) checkLinkCTA(v issues, d Destination) {
	if c.CallToAction != "" && !slices.Contains(CallsToActionFor(d), c.CallToAction) {
		v.add("callToAction", "invalid")
	}
}

func (c CreativeDraft) ResolvedCallToAction(d Destination) CallToAction {
	if cta := MessagingCallToAction(d); cta != "" {
		return cta
	}
	if c.CallToAction != "" {
		return c.CallToAction
	}
	switch d {
	case DestinationApp:
		return CTAInstallApp
	case DestinationInstantForm:
		return CTASignUp
	case DestinationCatalog:
		return CTAShopNow
	}
	return CTALearnMore
}
