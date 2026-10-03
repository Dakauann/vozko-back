package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"vozko/domain/advertising"
)

const creativePath = "POST /v26.0/act_9/adcreatives"

func imageSpec(destination advertising.Destination) advertising.CreativeSpec {
	draft := advertising.CreativeDraft{
		Format:      advertising.FormatImage,
		PrimaryText: "Oferta",
		Headline:    "Fale",
		Media:       advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "m1"},
	}
	return advertising.CreativeSpec{
		Name:         "CR",
		Identity:     advertising.Identity{PageID: "P1"},
		Destination:  destination,
		CallToAction: draft.ResolvedCallToAction(destination),
		Creative:     draft,
		Media:        advertising.UploadedMedia{ImageHashes: map[string]string{"m1": "h1"}, VideoIDs: map[string]string{}},
	}
}

func createCreative(t *testing.T, spec advertising.CreativeSpec, table map[string]string) (recordedCall, []recordedCall) {
	t.Helper()
	if table == nil {
		table = map[string]string{}
	}
	table[creativePath] = `{"id":"CR1"}`
	g, calls := gatewayWith(t, routes(t, table))
	id, err := g.CreateCreative(context.Background(), "tok", "9", spec)
	if err != nil || id != "CR1" {
		t.Fatalf("id %q err %v", id, err)
	}
	last := (*calls)[len(*calls)-1]
	if last.method != http.MethodPost || last.path != "/v26.0/act_9/adcreatives" || last.form.Get("name") != spec.Name {
		t.Fatalf("call = %+v", last)
	}
	return last, *calls
}

func TestCreateCreativeImageByDestination(t *testing.T) {
	tests := []struct {
		name  string
		spec  func() advertising.CreativeSpec
		story string
	}{
		{
			name: "whatsapp with greeting",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationWhatsApp)
				s.Creative.Greeting = "Olá!"
				return s
			},
			story: `{"page_id":"P1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","link":"https://api.whatsapp.com/send",
				"call_to_action":{"type":"WHATSAPP_MESSAGE","value":{"app_destination":"WHATSAPP"}},
				"page_welcome_message":"{\"type\":\"VISUAL_EDITOR\",\"version\":2,\"landing_screen_type\":\"welcome_message\",\"media_type\":\"text\",\"text_format\":{\"customer_action_type\":\"autofill_message\",\"message\":{\"text\":\"Olá!\",\"autofill_message\":{\"content\":\"Olá!\"}}}}"}}`,
		},
		{
			name: "instagram direct with ice breakers",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationInstagramDirect)
				s.Identity.InstagramUserID = "IG1"
				s.Creative.IceBreakers = []string{"Preço?", "Horário?"}
				return s
			},
			story: `{"page_id":"P1","instagram_user_id":"IG1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","link":"https://www.instagram.com/",
				"call_to_action":{"type":"INSTAGRAM_MESSAGE","value":{"app_destination":"INSTAGRAM_DIRECT"}},
				"page_welcome_message":"{\"type\":\"VISUAL_EDITOR\",\"version\":2,\"landing_screen_type\":\"welcome_message\",\"media_type\":\"text\",\"text_format\":{\"customer_action_type\":\"ice_breakers\",\"message\":{\"text\":\"Preço?\",\"ice_breakers\":[{\"title\":\"Preço?\"},{\"title\":\"Horário?\"}]}}}"}}`,
		},
		{
			name: "website",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationWebsite)
				s.Creative.Link, s.Creative.DisplayLink, s.Creative.Description = "https://loja.com/p", "loja.com", "Frete grátis"
				s.Creative.Greeting = "ignored"
				return s
			},
			story: `{"page_id":"P1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","description":"Frete grátis","caption":"loja.com",
				"link":"https://loja.com/p","call_to_action":{"type":"LEARN_MORE","value":{"link":"https://loja.com/p"}}}}`,
		},
		{
			name: "instant form",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationInstantForm)
				s.Creative.LeadFormID = "F1"
				return s
			},
			story: `{"page_id":"P1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","link":"http://fb.me/",
				"call_to_action":{"type":"SIGN_UP","value":{"link":"http://fb.me/","lead_gen_form_id":"F1"}}}}`,
		},
		{
			name: "app",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationApp)
				s.AppStoreURL = "https://apps.apple.com/app/id1"
				return s
			},
			story: `{"page_id":"P1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","link":"https://apps.apple.com/app/id1",
				"call_to_action":{"type":"INSTALL_MOBILE_APP","value":{"link":"https://apps.apple.com/app/id1"}}}}`,
		},
		{
			name: "awareness without button",
			spec: func() advertising.CreativeSpec {
				s := imageSpec(advertising.DestinationNone)
				s.CallToAction = advertising.CTANoButton
				return s
			},
			story: `{"page_id":"P1","link_data":{"image_hash":"h1","message":"Oferta","name":"Fale","link":"https://www.facebook.com/P1",
				"call_to_action":{"type":"NO_BUTTON"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call, _ := createCreative(t, tt.spec(), nil)
			jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), tt.story)
		})
	}
}

func enrollments(t *testing.T, raw string) map[string]string {
	t.Helper()
	features := decodeField(t, raw)["creative_features_spec"].(map[string]any)
	out := map[string]string{}
	for k, v := range features {
		out[k] = v.(map[string]any)["enroll_status"].(string)
	}
	return out
}

func TestCreateCreativeEnhancements(t *testing.T) {
	for _, on := range []bool{false, true} {
		spec := imageSpec(advertising.DestinationWhatsApp)
		spec.Creative.Enhancements = on
		call, _ := createCreative(t, spec, nil)
		got := enrollments(t, call.form.Get("degrees_of_freedom_spec"))
		if len(got) != len(enhancementFeatures) {
			t.Fatalf("features = %v", got)
		}
		for feature, status := range got {
			want := "OPT_OUT"
			if on && (feature == "image_touchups" || feature == "text_optimizations" || feature == "enhance_cta" || feature == "adapt_to_placement") {
				want = "OPT_IN"
			}
			if status != want {
				t.Fatalf("on=%v %s = %s", on, feature, status)
			}
		}
	}
}

func videoSpec() advertising.CreativeSpec {
	s := imageSpec(advertising.DestinationWebsite)
	s.Creative.Format = advertising.FormatVideo
	s.Creative.Link = "https://loja.com"
	s.Creative.Description = "Desc"
	s.Creative.Media = advertising.MediaRef{Kind: advertising.MediaVideo, MediaID: "m2"}
	s.Media = advertising.UploadedMedia{VideoIDs: map[string]string{"m2": "V2"}}
	return s
}

const readyVideo = `{"id":"V2","status":{"video_status":"ready"},"thumbnails":{"data":[{"uri":"https://thumb/v2","is_preferred":true}]}}`

func TestCreateCreativeVideoUsesPreferredThumbnail(t *testing.T) {
	call, calls := createCreative(t, videoSpec(), map[string]string{"GET /v26.0/V2": readyVideo})
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1","video_data":{"video_id":"V2","image_url":"https://thumb/v2",
		"message":"Oferta","title":"Fale","link_description":"Desc","call_to_action":{"type":"LEARN_MORE","value":{"link":"https://loja.com"}}}}`)
}

func TestCreateCreativeVideoNotReadyNeverCreates(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{"GET /v26.0/V2": `{"id":"V2","status":{"video_status":"processing"}}`}))
	_, err := g.CreateCreative(context.Background(), "tok", "9", videoSpec())
	if !errors.Is(err, advertising.ErrVideoNotReady) || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestCreateCreativeVideoWithoutThumbnailFails(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{"GET /v26.0/V2": `{"id":"V2","status":{"video_status":"ready"},"thumbnails":{"data":[{"uri":"https://t","is_preferred":false}]}}`}))
	if _, err := g.CreateCreative(context.Background(), "tok", "9", videoSpec()); err == nil || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestCreateCreativeCarousel(t *testing.T) {
	spec := imageSpec(advertising.DestinationWebsite)
	spec.Creative.Format = advertising.FormatCarousel
	spec.Creative.Cards = []advertising.CarouselCard{
		{Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "m1"}, Headline: "Card 1", Link: "https://loja.com/a"},
		{Media: advertising.MediaRef{Kind: advertising.MediaVideo, MediaID: "m2"}, Headline: "Card 2", Description: "d"},
	}
	spec.Media = advertising.UploadedMedia{ImageHashes: map[string]string{"m1": "h1"}, VideoIDs: map[string]string{"m2": "V2"}}
	call, _ := createCreative(t, spec, map[string]string{"GET /v26.0/V2": readyVideo})
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1","link_data":{"link":"https://loja.com/a","message":"Oferta",
		"call_to_action":{"type":"LEARN_MORE","value":{"link":"https://loja.com/a"}},
		"multi_share_optimized":true,"multi_share_end_card":false,
		"child_attachments":[
			{"link":"https://loja.com/a","name":"Card 1","image_hash":"h1","call_to_action":{"type":"LEARN_MORE","value":{"link":"https://loja.com/a"}}},
			{"link":"https://loja.com/a","name":"Card 2","description":"d","video_id":"V2","picture":"https://thumb/v2","call_to_action":{"type":"LEARN_MORE","value":{"link":"https://loja.com/a"}}}]}}`)
}

func TestCreateCreativeExistingPosts(t *testing.T) {
	facebook := imageSpec(advertising.DestinationOnPost)
	facebook.Creative = advertising.CreativeDraft{Format: advertising.FormatExistingPost, PostID: "P1_55"}
	facebook.CallToAction = facebook.Creative.ResolvedCallToAction(advertising.DestinationOnPost)
	call, _ := createCreative(t, facebook, nil)
	expectForm(t, call, map[string]string{"object_story_id": "P1_55"}, "object_story_spec", "object_id", "call_to_action")

	instagram := imageSpec(advertising.DestinationWebsite)
	instagram.Identity.InstagramUserID = "IG1"
	instagram.Creative = advertising.CreativeDraft{Format: advertising.FormatExistingPost, InstagramMediaID: "IGM1", Link: "https://loja.com", CallToAction: advertising.CTAShopNow}
	instagram.CallToAction = instagram.Creative.ResolvedCallToAction(advertising.DestinationWebsite)
	call, _ = createCreative(t, instagram, nil)
	expectForm(t, call, map[string]string{
		"object_id": "P1", "instagram_user_id": "IG1", "source_instagram_media_id": "IGM1",
		"call_to_action": `{"type":"SHOP_NOW","value":{"link":"https://loja.com"}}`,
	}, "object_story_spec", "object_story_id")
}

func TestCreateCreativeInstagramPostNeedsAccount(t *testing.T) {
	spec := imageSpec(advertising.DestinationOnPost)
	spec.Creative = advertising.CreativeDraft{Format: advertising.FormatExistingPost, InstagramMediaID: "IGM1"}
	g, calls := gatewayWith(t, ok(`{"id":"CR1"}`))
	if _, err := g.CreateCreative(context.Background(), "tok", "9", spec); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func collectionSpec() advertising.CreativeSpec {
	s := imageSpec(advertising.DestinationCatalog)
	s.Creative.Format = advertising.FormatCollection
	s.Creative.InstantExperience = "CV1"
	return s
}

func TestCreateCreativeCollection(t *testing.T) {
	canvas := `{"body_elements":[{"id":"E0","element_type":"BUTTON"},{"id":"E1","element_type":"PHOTO"},{"id":"E2","element_type":"PHOTO"},
		{"id":"E3","element_type":"PHOTO"},{"id":"E4","element_type":"PHOTO"},{"id":"E5","element_type":"PHOTO"}]}`
	call, calls := createCreative(t, collectionSpec(), map[string]string{"GET /v26.0/CV1": canvas})
	if calls[0].query.Get("fields") != "body_elements{id,element_type}" {
		t.Fatalf("canvas call = %+v", calls[0])
	}
	crop := `"element_crops":{"100x100":[[0,0],[100,100]]}`
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1","link_data":{"link":"https://fb.com/canvas_doc/CV1",
		"message":"Oferta","name":"Fale","image_hash":"h1","call_to_action":{"type":"SHOP_NOW","value":{"link":"https://fb.com/canvas_doc/CV1"}},
		"collection_thumbnails":[{"element_id":"E1",`+crop+`},{"element_id":"E2",`+crop+`},{"element_id":"E3",`+crop+`},{"element_id":"E4",`+crop+`}]}}`)
}

func TestCreateCreativeCollectionSkipsTheHeroPhoto(t *testing.T) {
	canvas := `{"body_elements":[{"id":"H0","element_type":"PHOTO"},{"id":"E1","element_type":"PHOTO"},{"id":"E2","element_type":"PHOTO"},
		{"id":"E3","element_type":"PHOTO"},{"id":"E4","element_type":"PHOTO"}]}`
	call, _ := createCreative(t, collectionSpec(), map[string]string{"GET /v26.0/CV1": canvas})
	var story struct {
		LinkData struct {
			Thumbnails []struct {
				ElementID string `json:"element_id"`
			} `json:"collection_thumbnails"`
		} `json:"link_data"`
	}
	if err := json.Unmarshal([]byte(call.form.Get("object_story_spec")), &story); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, thumb := range story.LinkData.Thumbnails {
		ids = append(ids, thumb.ElementID)
	}
	if strings.Join(ids, ",") != "E1,E2,E3,E4" {
		t.Fatalf("thumbnails %v", ids)
	}
}

func TestCreateCreativeCollectionNeedsFourPhotos(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{"GET /v26.0/CV1": `{"body_elements":{"data":[{"id":"E0","element_type":"BUTTON"},{"id":"E1","element_type":"PHOTO"}]}}`}))
	_, err := g.CreateCreative(context.Background(), "tok", "9", collectionSpec())
	if err == nil || !strings.Contains(err.Error(), "1 photos") || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestCreateCreativeCatalog(t *testing.T) {
	spec := imageSpec(advertising.DestinationCatalog)
	spec.Identity.InstagramUserID = "IG1"
	spec.ProductSetID = "PS1"
	spec.Creative = advertising.CreativeDraft{Format: advertising.FormatCatalog, PrimaryText: "{{product.name}}", Headline: "{{product.price}}", Link: "https://loja.com"}
	spec.CallToAction = spec.Creative.ResolvedCallToAction(advertising.DestinationCatalog)
	call, _ := createCreative(t, spec, nil)
	expectForm(t, call, map[string]string{"product_set_id": "PS1"})
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1","instagram_user_id":"IG1","template_data":{
		"link":"https://loja.com","message":"{{product.name}}","name":"{{product.price}}","call_to_action":{"type":"SHOP_NOW"},"multi_share_end_card":true}}`)
}

func TestCreateCreativeDynamicAssetFeed(t *testing.T) {
	spec := groupsSpec()
	spec.AssetMode = advertising.AssetsDynamic
	spec.Creative.DisplayLink = "loja.com"
	call, _ := createCreative(t, spec, map[string]string{"GET /v26.0/V2": readyVideo})
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1"}`)
	jsonEqual(t, "asset_feed_spec", call.form.Get("asset_feed_spec"), `{"images":[{"hash":"h1"}],"videos":[{"video_id":"V2","thumbnail_url":"https://thumb/v2"}],
		"bodies":[{"text":"Promo"}],"titles":[{"text":"50% off"}],"descriptions":[{"text":"Hoje"}],
		"link_urls":[{"website_url":"https://loja.com","display_url":"loja.com"}],"call_to_action_types":["SHOP_NOW"],"ad_formats":["AUTOMATIC_FORMAT"]}`)
}

func TestCreateCreativeAssetGroupsCarriesIdentityOnly(t *testing.T) {
	spec := groupsSpec()
	spec.Identity.InstagramUserID = "IG1"
	call, calls := createCreative(t, spec, nil)
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	jsonEqual(t, "object_story_spec", call.form.Get("object_story_spec"), `{"page_id":"P1","instagram_user_id":"IG1"}`)
	expectForm(t, call, nil, "asset_feed_spec")
}

func TestCreateCreativeFailsBeforeCallingMeta(t *testing.T) {
	tests := map[string]func(*advertising.CreativeSpec){
		"image not uploaded":      func(s *advertising.CreativeSpec) { s.Media = advertising.UploadedMedia{} },
		"no page":                 func(s *advertising.CreativeSpec) { s.Identity.PageID = "" },
		"flexible without mode":   func(s *advertising.CreativeSpec) { s.Creative.Format = advertising.FormatFlexible },
		"mode on a single image":  func(s *advertising.CreativeSpec) { s.AssetMode = advertising.AssetsDynamic },
		"unknown destination":     func(s *advertising.CreativeSpec) { s.Destination = "SMS" },
		"website without link":    func(s *advertising.CreativeSpec) { s.Destination = advertising.DestinationWebsite },
		"instant form without id": func(s *advertising.CreativeSpec) { s.Destination = advertising.DestinationInstantForm },
		"missing call to action":  func(s *advertising.CreativeSpec) { s.CallToAction = "" },
		"video ref on image":      func(s *advertising.CreativeSpec) { s.Creative.Media.Kind = advertising.MediaVideo },
		"unknown format":          func(s *advertising.CreativeSpec) { s.Creative.Format = "STORY" },
		"collection video cover": func(s *advertising.CreativeSpec) {
			*s = collectionSpec()
			s.Creative.Media.Kind = advertising.MediaVideo
		},
		"dynamic with lead form": func(s *advertising.CreativeSpec) {
			*s = groupsSpec()
			s.AssetMode, s.Destination = advertising.AssetsDynamic, advertising.DestinationInstantForm
		},
		"catalog without products": func(s *advertising.CreativeSpec) { s.Creative.Format = advertising.FormatCatalog },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			spec := imageSpec(advertising.DestinationWhatsApp)
			edit(&spec)
			g, calls := gatewayWith(t, ok(`{"id":"CR1"}`))
			if _, err := g.CreateCreative(context.Background(), "tok", "9", spec); err == nil || len(*calls) != 0 {
				t.Fatalf("err %v calls %d", err, len(*calls))
			}
		})
	}
}
