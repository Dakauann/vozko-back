package copilottools

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/advertising"
	"vozko/domain/tools"
)

const (
	budgetLevelCampaign = "campaign"
	budgetLevelAdSet    = "adset"
	budgetKindDaily     = "daily"
	budgetKindLifetime  = "lifetime"
	placementsAutomatic = "automatic"
)

type adCardArgs struct {
	MediaID     string `json:"media_id" req:"true" desc:"media_id de generate_image ou de uma imagem ou vídeo anexado"`
	MediaKind   string `json:"media_kind" enum:"image,video" desc:"image (padrão) ou video"`
	Headline    string `json:"headline" desc:"título do cartão"`
	Description string `json:"description" desc:"descrição curta do cartão"`
	Link        string `json:"link" desc:"endereço https do cartão; vazio usa o link do anúncio"`
}

type adCreativeArgs struct {
	Link         string       `json:"link" desc:"endereço https do site (obrigatório para WEBSITE e CATALOG), ex.: https://vozkoia.com"`
	DisplayLink  string       `json:"display_link" desc:"link curto mostrado no anúncio, opcional, ex.: vozkoia.com"`
	CallToAction string       `json:"call_to_action" desc:"botão para WEBSITE, ON_AD, APP ou CATALOG, ex.: LEARN_MORE, SIGN_UP, CONTACT_US, SHOP_NOW, BOOK_NOW, INSTALL_MOBILE_APP; vazio usa o padrão"`
	LeadFormID   string       `json:"lead_form_id" desc:"lead_form_id de list_lead_forms (obrigatório para ON_AD)"`
	Format       string       `json:"format" enum:"IMAGE,VIDEO,CAROUSEL,FLEXIBLE,EXISTING_POST,CATALOG" desc:"IMAGE (uma imagem), VIDEO (um vídeo), CAROUSEL (2 a 10 cartões em cards), FLEXIBLE (várias mídias e variações de texto que a Meta combina), EXISTING_POST (uma publicação da página, de list_page_posts) ou CATALOG (produtos do catálogo, com CATALOG)"`
	PrimaryText  string       `json:"primary_text" desc:"texto principal do anúncio (obrigatório, exceto em FLEXIBLE e EXISTING_POST)"`
	Headline     string       `json:"headline" desc:"título curto (até 40 caracteres é o ideal)"`
	Description  string       `json:"description" desc:"descrição curta opcional"`
	MediaID      string       `json:"media_id" id:"true" desc:"media_id de generate_image ou de uma imagem ou vídeo anexado, do mesmo tipo de format (IMAGE e VIDEO)"`
	Cards        []adCardArgs `json:"cards" desc:"cartões do CAROUSEL, de 2 a 10, na ordem"`
	MediaIDs     []string     `json:"media_ids" id:"true" desc:"imagens do FLEXIBLE: media_id de generate_image ou de imagens anexadas"`
	VideoIDs     []string     `json:"video_ids" id:"true" desc:"vídeos do FLEXIBLE: media_id de vídeos anexados"`
	Texts        []string     `json:"texts" desc:"FLEXIBLE: de 1 a 5 variações do texto principal"`
	Headlines    []string     `json:"headlines" desc:"FLEXIBLE: até 5 variações do título"`
	Descriptions []string     `json:"descriptions" desc:"FLEXIBLE: até 5 variações da descrição"`
	PostID       string       `json:"post_id" desc:"post_id exato de list_page_posts (EXISTING_POST)"`
	PostPlatform string       `json:"post_platform" enum:"facebook,instagram" desc:"de onde vem a publicação de EXISTING_POST: facebook (padrão) ou instagram"`
	Greeting     string       `json:"greeting" desc:"mensagem que já vem escrita para o cliente enviar (WhatsApp, Messenger, Instagram)"`
	IceBreakers  []string     `json:"ice_breakers" desc:"até 3 perguntas prontas para o cliente tocar (WhatsApp, Messenger, Instagram)"`
	Enhancements *bool        `json:"enhancements" desc:"melhorias de criativo Advantage+: true deixa a Meta cortar, expandir e ajustar a imagem e trocar a ordem dos textos; false mantém o anúncio exatamente como foi montado (use false em criativos com texto ou telas na imagem); vazio mantém o atual"`
}

func adDraftDefinition(name, description string, args any) tools.Definition {
	def := definition(name, description, args)
	cards := def.Parameters["cards"]
	properties, required := structParams(reflect.TypeOf(adCardArgs{}), nil)
	cards.Items = &tools.ParameterItems{Type: "object", Properties: properties, Required: required}
	def.Parameters["cards"] = cards
	return def
}

type draftPart struct {
	keys  []string
	apply func(a adDraftArgs, account *advertising.AdAccount, d *advertising.AdDraft) error
}

var creativeArgKeys = []string{
	"format", "primary_text", "headline", "description", "media_id", "cards", "media_ids", "video_ids", "texts", "headlines", "descriptions",
	"post_id", "post_platform", "link", "display_link", "call_to_action", "lead_form_id", "greeting", "ice_breakers", "enhancements",
}

var draftParts = []draftPart{
	{keys: creativeArgKeys, apply: adDraftArgs.applyCreative},
	{keys: []string{"objective", "destination", "goal"}, apply: adDraftArgs.applyRoute},
	{keys: []string{"campaign_name"}, apply: adDraftArgs.applyName},
	{keys: []string{"special_category"}, apply: adDraftArgs.applySpecialCategory},
	{keys: []string{"page_id", "instagram_user_id"}, apply: adDraftArgs.applyIdentity},
	{keys: []string{"whatsapp_number", "pixel_id", "pixel_event", "app_id", "app_store_url", "catalog_id", "product_set_id"}, apply: adDraftArgs.applyPromotion},
	{keys: []string{"budget", "daily_budget", "budget_kind", "budget_level", "bid_strategy", "bid_amount", "roas"}, apply: adDraftArgs.applyBudget},
	{keys: []string{"start_date"}, apply: adDraftArgs.applyStart},
	{keys: []string{"end_date"}, apply: adDraftArgs.applyEnd},
	{keys: []string{"locations"}, apply: adDraftArgs.applyLocations},
	{keys: []string{"age_min", "age_max"}, apply: adDraftArgs.applyAge},
	{keys: []string{"genders"}, apply: adDraftArgs.applyGenders},
	{keys: []string{"interests"}, apply: adDraftArgs.applyInterests},
	{keys: []string{"custom_audiences", "excluded_custom_audiences"}, apply: adDraftArgs.applyCustomAudiences},
	{keys: []string{"placements"}, apply: adDraftArgs.applyPlacements},
	{keys: []string{"keep_paused"}, apply: adDraftArgs.applyKeepPaused},
}

func (a adDraftArgs) apply(account *advertising.AdAccount, d *advertising.AdDraft, touched func(string) bool) error {
	for _, part := range draftParts {
		if !slices.ContainsFunc(part.keys, touched) {
			continue
		}
		if err := part.apply(a, account, d); err != nil {
			return err
		}
	}
	d.Normalize()
	return nil
}

func (a adDraftArgs) draft(account *advertising.AdAccount) (advertising.AdDraft, error) {
	d := advertising.AdDraft{AdAccountID: account.ID}
	err := a.apply(account, &d, func(string) bool { return true })
	return d, err
}

func patchDraft(stored advertising.AdDraft, account *advertising.AdAccount, changes map[string]interface{}) (advertising.AdDraft, error) {
	touched := func(key string) bool {
		_, ok := changes[key]
		return ok
	}
	effective := draftArgs(stored, account)
	if (touched("objective") || touched("destination")) && !touched("goal") {
		effective.Goal = ""
	}
	if touched("daily_budget") && !touched("budget") {
		effective.Budget = 0
		if !touched("budget_kind") {
			effective.BudgetKind = budgetKindDaily
		}
	}
	if touched("cards") {
		effective.Cards = nil
	}
	if err := decodeArgs(changes, &effective); err != nil {
		return advertising.AdDraft{}, err
	}
	d := stored
	d.Ads = append([]advertising.AdItem(nil), stored.Ads...)
	if err := effective.apply(account, &d, touched); err != nil {
		return advertising.AdDraft{}, err
	}
	return d, nil
}

func (a adDraftArgs) applyCreative(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	switch len(d.Ads) {
	case 0:
		d.Ads = []advertising.AdItem{{}}
	case 1:
	default:
		return fmt.Errorf("%w: o rascunho tem %d anúncios; mude os criativos na tela do editor", errInvalidArgs, len(d.Ads))
	}
	creative, err := a.creative(d.Ads[0].Creative)
	if err != nil {
		return err
	}
	d.Ads[0].Creative = creative
	return nil
}

func (a adCreativeArgs) creative(kept advertising.CreativeDraft) (advertising.CreativeDraft, error) {
	format := advertising.CreativeFormat(a.Format)
	c := advertising.CreativeDraft{
		Format: format, PrimaryText: a.PrimaryText, Headline: a.Headline, Description: a.Description,
		Link: strings.TrimSpace(a.Link), DisplayLink: strings.TrimSpace(a.DisplayLink),
		CallToAction: advertising.CallToAction(a.CallToAction), LeadFormID: strings.TrimSpace(a.LeadFormID),
		Greeting: a.Greeting, IceBreakers: a.IceBreakers,
		InstantExperience: kept.InstantExperience, Enhancements: kept.Enhancements,
	}
	if a.Enhancements != nil {
		c.Enhancements = *a.Enhancements
	}
	switch format {
	case advertising.FormatImage, advertising.FormatVideo:
		kind, _ := format.SingleMediaKind()
		c.Media = advertising.MediaRef{Kind: kind, MediaID: a.MediaID}
	case advertising.FormatCarousel:
		cards, err := a.cards()
		if err != nil {
			return advertising.CreativeDraft{}, err
		}
		c.Cards = cards
	case advertising.FormatFlexible:
		c.Medias = append(mediaRefs(advertising.MediaImage, a.MediaIDs), mediaRefs(advertising.MediaVideo, a.VideoIDs)...)
		c.Texts, c.Headlines, c.Descriptions = a.Texts, a.Headlines, a.Descriptions
	case advertising.FormatExistingPost:
		if a.PostPlatform == advertising.PlatformInstagram {
			c.InstagramMediaID = strings.TrimSpace(a.PostID)
		} else {
			c.PostID = strings.TrimSpace(a.PostID)
		}
	case advertising.FormatCatalog:
	default:
		return advertising.CreativeDraft{}, fmt.Errorf("%w: format deve ser IMAGE, VIDEO, CAROUSEL, FLEXIBLE, EXISTING_POST ou CATALOG", errInvalidArgs)
	}
	return c, knownMedia(c, kept)
}

func knownMedia(c, kept advertising.CreativeDraft) error {
	hosted := kept.MetaMediaRefs()
	for _, ref := range c.MediaRefs() {
		if _, onMeta := ref.MetaID(); onMeta {
			if !slices.Contains(hosted, ref) {
				return fmt.Errorf("%w: media_id %q não é do anúncio atual; use o media_id exato de generate_image ou de um anexo", errInvalidArgs, ref.MediaID)
			}
			continue
		}
		if _, err := uuid.Parse(ref.MediaID); err != nil {
			return fmt.Errorf("%w: media_id %q não existe; use o media_id exato de generate_image ou de um anexo", errInvalidArgs, ref.MediaID)
		}
	}
	return nil
}

func mediaRefs(kind advertising.MediaKind, ids []string) []advertising.MediaRef {
	out := make([]advertising.MediaRef, 0, len(ids))
	for _, id := range ids {
		out = append(out, advertising.MediaRef{Kind: kind, MediaID: strings.TrimSpace(id)})
	}
	return out
}

func (a adCreativeArgs) cards() ([]advertising.CarouselCard, error) {
	out := make([]advertising.CarouselCard, 0, len(a.Cards))
	for _, card := range a.Cards {
		id := strings.TrimSpace(card.MediaID)
		kind := advertising.MediaImage
		switch card.MediaKind {
		case "", string(advertising.MediaImage):
		case string(advertising.MediaVideo):
			kind = advertising.MediaVideo
		default:
			return nil, fmt.Errorf("%w: cards.media_kind deve ser image ou video", errInvalidArgs)
		}
		out = append(out, advertising.CarouselCard{
			Media: advertising.MediaRef{Kind: kind, MediaID: id}, Headline: strings.TrimSpace(card.Headline),
			Description: strings.TrimSpace(card.Description), Link: strings.TrimSpace(card.Link),
		})
	}
	return out, nil
}

func (a adDraftArgs) applyRoute(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	objective, destination, goal, err := a.route()
	if err != nil {
		return err
	}
	d.Campaign.Objective, d.AdSet.Destination, d.AdSet.Goal = objective, destination, goal
	return nil
}

func (a adDraftArgs) applyName(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	previous := d.Campaign.Name
	d.Campaign.Name = a.CampaignName
	if d.AdSet.Name == previous {
		d.AdSet.Name = ""
	}
	for i := range d.Ads {
		if d.Ads[i].Name == previous {
			d.Ads[i].Name = ""
		}
	}
	return nil
}

func (a adDraftArgs) applySpecialCategory(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	d.Campaign.SpecialCategory = advertising.SpecialCategory(a.SpecialCategory)
	return nil
}

func (a adDraftArgs) applyIdentity(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	d.Identity = advertising.Identity{PageID: a.PageID, InstagramUserID: a.InstagramUserID}
	return nil
}

func (a adDraftArgs) applyPromotion(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	s := &d.AdSet
	s.WhatsAppNumber = a.WhatsAppNumber
	s.PixelID, s.PixelEvent = strings.TrimSpace(a.PixelID), advertising.PixelEvent(a.PixelEvent)
	s.AppID, s.AppStoreURL = strings.TrimSpace(a.AppID), strings.TrimSpace(a.AppStoreURL)
	s.CatalogID, s.ProductSetID = strings.TrimSpace(a.CatalogID), strings.TrimSpace(a.ProductSetID)
	return nil
}

func (a adDraftArgs) applyBudget(account *advertising.AdAccount, d *advertising.AdDraft) error {
	budget, err := a.budget(account.Currency)
	if err != nil {
		return err
	}
	bid, err := a.bid(account.Currency)
	if err != nil {
		return err
	}
	d.Campaign.Budget, d.Campaign.Bid, d.AdSet.Budget, d.AdSet.Bid = nil, advertising.Bid{}, nil, advertising.Bid{}
	switch a.BudgetLevel {
	case budgetLevelCampaign:
		d.Campaign.Budget, d.Campaign.Bid = budget, bid
	case "", budgetLevelAdSet:
		d.AdSet.Budget, d.AdSet.Bid = budget, bid
	default:
		return fmt.Errorf("%w: budget_level deve ser adset ou campaign", errInvalidArgs)
	}
	return nil
}

func (a adDraftArgs) budget(currency string) (*advertising.Budget, error) {
	kind := advertising.BudgetDaily
	if a.BudgetKind == budgetKindLifetime {
		kind = advertising.BudgetLifetime
	}
	amount := a.Budget
	if a.DailyBudget != 0 {
		if amount != 0 || kind != advertising.BudgetDaily {
			return nil, fmt.Errorf("%w: daily_budget é só para orçamento diário; use budget com budget_kind", errInvalidArgs)
		}
		amount = a.DailyBudget
	}
	minor, err := advertising.AmountToMinor(currency, amount)
	if err != nil {
		return nil, fmt.Errorf("%w: budget deve ser maior que zero", errInvalidArgs)
	}
	return &advertising.Budget{Kind: kind, Amount: minor}, nil
}

func (a adDraftArgs) bid(currency string) (advertising.Bid, error) {
	bid := advertising.Bid{Strategy: advertising.BidStrategy(a.BidStrategy)}.Normalized()
	if bid.NeedsAmount() && a.BidAmount != 0 {
		minor, err := advertising.AmountToMinor(currency, a.BidAmount)
		if err != nil {
			return advertising.Bid{}, fmt.Errorf("%w: bid_amount deve ser maior que zero", errInvalidArgs)
		}
		bid.Amount = minor
	}
	if bid.Strategy == advertising.BidMinROAS {
		bid.ROASFloor = a.ROAS
	}
	return bid, nil
}

func (a adDraftArgs) applyStart(account *advertising.AdAccount, d *advertising.AdDraft) error {
	start, _, err := dayBounds(account, a.StartDate, "start_date")
	if err != nil {
		return err
	}
	d.AdSet.StartAt = start
	return nil
}

func (a adDraftArgs) applyEnd(account *advertising.AdAccount, d *advertising.AdDraft) error {
	end, err := a.endAt(account)
	if err != nil {
		return err
	}
	d.AdSet.EndAt = end
	return nil
}

func (a adDraftArgs) endAt(account *advertising.AdAccount) (*time.Time, error) {
	_, end, err := dayBounds(account, a.EndDate, "end_date")
	return end, err
}

func dayBounds(account *advertising.AdAccount, raw, name string) (*time.Time, *time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, nil
	}
	day, err := advertising.ParseDay(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s deve ser YYYY-MM-DD", errInvalidArgs, name)
	}
	loc, err := account.Location()
	if err != nil {
		return nil, nil, err
	}
	start, end := advertising.DateRange{Since: day, Until: day}.Bounds(loc)
	return &start, &end, nil
}

func (a adDraftArgs) applyLocations(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	locations, err := parseLocations(a.Locations)
	if err != nil {
		return err
	}
	d.AdSet.Targeting.Locations = locations
	return nil
}

func (a adDraftArgs) applyAge(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	d.AdSet.Targeting.AgeMin, d.AdSet.Targeting.AgeMax = a.AgeMin, a.AgeMax
	return nil
}

func (a adDraftArgs) applyGenders(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	d.AdSet.Targeting.Genders = genders(a.Genders)
	return nil
}

func (a adDraftArgs) applyInterests(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	interests, err := parseTargetRefs(a.Interests)
	if err != nil {
		return err
	}
	d.AdSet.Targeting.Interests = interests
	return nil
}

func (a adDraftArgs) applyCustomAudiences(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	included, err := parseAudienceRefs(a.CustomAudiences, "custom_audiences")
	if err != nil {
		return err
	}
	excluded, err := parseAudienceRefs(a.ExcludedCustomAudiences, "excluded_custom_audiences")
	if err != nil {
		return err
	}
	d.AdSet.Targeting.CustomAudiences, d.AdSet.Targeting.ExcludedCustomAudiences = included, excluded
	return nil
}

func parseAudienceRefs(raw []string, name string) ([]advertising.TargetRef, error) {
	out := make([]advertising.TargetRef, 0, len(raw))
	for _, item := range raw {
		id, label, _ := strings.Cut(strings.TrimSpace(item), ":")
		if _, err := strconv.ParseUint(id, 10, 64); err != nil {
			return nil, fmt.Errorf("%w: %s %q não existe; use o id exato de um público da conta, nunca invente", errInvalidArgs, name, item)
		}
		out = append(out, advertising.TargetRef{ID: id, Name: label})
	}
	return out, nil
}

func (a adDraftArgs) applyPlacements(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	placements, err := placementsKeepingDevices(a.Placements, d.AdSet.Placements)
	if err != nil {
		return err
	}
	d.AdSet.Placements = placements
	return nil
}

func placementsKeepingDevices(raw []string, current advertising.Placements) (advertising.Placements, error) {
	placements, err := parsePlacements(raw)
	if err != nil {
		return advertising.Placements{}, err
	}
	if !placements.Automatic {
		placements.Devices = current.Devices
	}
	return placements, nil
}

func parsePlacements(raw []string) (advertising.Placements, error) {
	platforms := make([]string, 0, len(raw))
	automatic := false
	for _, item := range raw {
		switch platform := strings.ToLower(strings.TrimSpace(item)); platform {
		case "":
		case placementsAutomatic:
			automatic = true
		default:
			platforms = append(platforms, platform)
		}
	}
	switch {
	case automatic && len(platforms) > 0:
		return advertising.Placements{}, fmt.Errorf("%w: placements é automatic ou uma lista de plataformas, não os dois", errInvalidArgs)
	case len(platforms) == 0:
		return advertising.Placements{Automatic: true}, nil
	}
	return advertising.Placements{Platforms: platforms}, nil
}

func (a adDraftArgs) applyKeepPaused(_ *advertising.AdAccount, d *advertising.AdDraft) error {
	d.KeepPaused = a.KeepPaused
	return nil
}

func draftArgs(d advertising.AdDraft, account *advertising.AdAccount) adDraftArgs {
	s, t := d.AdSet, d.AdSet.Targeting
	a := adDraftArgs{
		AdAccountID: d.AdAccountID, CampaignName: d.Campaign.Name,
		Objective: string(d.Campaign.Objective), Destination: string(s.Destination), Goal: string(s.Goal),
		PageID: d.Identity.PageID, InstagramUserID: d.Identity.InstagramUserID, WhatsAppNumber: s.WhatsAppNumber,
		AppID: s.AppID, AppStoreURL: s.AppStoreURL, CatalogID: s.CatalogID, ProductSetID: s.ProductSetID,
		PixelID: s.PixelID, PixelEvent: string(s.PixelEvent), SpecialCategory: string(d.Campaign.SpecialCategory),
		Locations: locationArgs(t.Locations), AgeMin: t.AgeMin, AgeMax: t.AgeMax, Genders: genderArgs(t.Genders),
		Interests: refArgs(t.Interests), CustomAudiences: refArgs(t.CustomAudiences), ExcludedCustomAudiences: refArgs(t.ExcludedCustomAudiences),
		Placements: placementArgs(s.Placements), KeepPaused: d.KeepPaused,
	}
	a.setMoney(d, account.Currency)
	a.setDates(s, account)
	if len(d.Ads) > 0 {
		a.setCreative(d.Ads[0].Creative)
	}
	return a
}

func (a *adDraftArgs) setMoney(d advertising.AdDraft, currency string) {
	budget, bid := d.AdSet.Budget, d.AdSet.Bid
	a.BudgetLevel = budgetLevelAdSet
	if d.Campaign.Budget != nil {
		budget, bid, a.BudgetLevel = d.Campaign.Budget, d.Campaign.Bid, budgetLevelCampaign
	}
	if budget != nil {
		a.Budget, a.BudgetKind = minorAmount(currency, budget.Amount), budgetKindDaily
		if budget.Kind == advertising.BudgetLifetime {
			a.BudgetKind = budgetKindLifetime
		}
	}
	if bid.Strategy != "" && bid.Strategy != advertising.BidLowestCost {
		a.BidStrategy, a.BidAmount, a.ROAS = string(bid.Strategy), minorAmount(currency, bid.Amount), bid.ROASFloor
	}
}

func minorAmount(currency string, minor int64) float64 {
	micros, err := advertising.MinorToMicros(currency, minor)
	if err != nil {
		return 0
	}
	return advertising.MicrosToAmount(micros)
}

func (a *adDraftArgs) setDates(s advertising.AdSetDraft, account *advertising.AdAccount) {
	loc, err := account.Location()
	if err != nil {
		return
	}
	if s.StartAt != nil {
		a.StartDate = s.StartAt.In(loc).Format(advertising.DayLayout)
	}
	if s.EndAt != nil {
		a.EndDate = lastDay(*s.EndAt, loc).Format(advertising.DayLayout)
	}
}

func lastDay(endAt time.Time, loc *time.Location) time.Time {
	return endAt.In(loc).Add(-time.Nanosecond)
}

func (a *adCreativeArgs) setCreative(c advertising.CreativeDraft) {
	a.Format, a.PrimaryText, a.Headline, a.Description = string(c.Format), c.PrimaryText, c.Headline, c.Description
	a.Link, a.DisplayLink, a.CallToAction, a.LeadFormID = c.Link, c.DisplayLink, string(c.CallToAction), c.LeadFormID
	a.Greeting, a.IceBreakers = c.Greeting, c.IceBreakers
	a.MediaID = c.Media.MediaID
	a.Texts, a.Headlines, a.Descriptions = c.Texts, c.Headlines, c.Descriptions
	for _, m := range c.Medias {
		if m.Kind == advertising.MediaVideo {
			a.VideoIDs = append(a.VideoIDs, m.MediaID)
		} else {
			a.MediaIDs = append(a.MediaIDs, m.MediaID)
		}
	}
	for _, card := range c.Cards {
		a.Cards = append(a.Cards, adCardArgs{MediaID: card.Media.MediaID, MediaKind: string(card.Media.Kind), Headline: card.Headline, Description: card.Description, Link: card.Link})
	}
	switch {
	case c.InstagramMediaID != "":
		a.PostID, a.PostPlatform = c.InstagramMediaID, advertising.PlatformInstagram
	case c.PostID != "":
		a.PostID, a.PostPlatform = c.PostID, advertising.PlatformFacebook
	}
}

func locationArgs(locations []advertising.GeoLocation) []string {
	out := make([]string, 0, len(locations))
	for _, l := range locations {
		out = append(out, string(l.Kind)+":"+l.Key)
	}
	return out
}

func refArgs(refs []advertising.TargetRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Name == "" {
			out = append(out, r.ID)
			continue
		}
		out = append(out, r.ID+":"+r.Name)
	}
	return out
}

func genderArgs(values []int) []string {
	out := make([]string, 0, len(values))
	for _, g := range values {
		switch g {
		case advertising.GenderMale:
			out = append(out, "male")
		case advertising.GenderFemale:
			out = append(out, "female")
		}
	}
	return out
}

func placementArgs(p advertising.Placements) []string {
	if p.Automatic {
		return []string{placementsAutomatic}
	}
	return append([]string(nil), p.Platforms...)
}
