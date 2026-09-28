package facebook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vozko/domain/cache"
	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

const (
	bucketProfile = "fb_messenger_profile"
	profilePath   = "/me/messenger_profile"
	profileFields = "greeting,get_started,ice_breakers,persistent_menu"
)

type ProfileConfig struct {
	Graph              GraphConfig
	RateLimiterFactory cache.RateLimiterFactory
}

type profileService struct {
	client   *meta.Client
	throttle *meta.Throttle
}

func NewProfileService(cfg ProfileConfig) (fbdomain.ProfileSettingsService, error) {
	client, err := newGraphClient(cfg.Graph, GraphHost)
	if err != nil {
		return nil, err
	}
	throttle, err := meta.NewThrottle(cfg.RateLimiterFactory, meta.Bucket{Name: bucketProfile, Max: 10, Window: 10 * time.Minute})
	if err != nil {
		return nil, err
	}
	return &profileService{client: client, throttle: throttle}, nil
}

type graphText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type graphPayload struct {
	Payload string `json:"payload"`
}

type graphIceBreaker struct {
	Question string `json:"question"`
	Payload  string `json:"payload"`
}

type graphIceBreakers struct {
	Locale        string            `json:"locale"`
	CallToActions []graphIceBreaker `json:"call_to_actions"`
}

type graphMenuItem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

type graphMenu struct {
	Locale                string          `json:"locale"`
	ComposerInputDisabled bool            `json:"composer_input_disabled"`
	CallToActions         []graphMenuItem `json:"call_to_actions"`
}

type graphProfile struct {
	Greeting       []graphText        `json:"greeting,omitempty"`
	GetStarted     *graphPayload      `json:"get_started,omitempty"`
	IceBreakers    []graphIceBreakers `json:"ice_breakers,omitempty"`
	PersistentMenu []graphMenu        `json:"persistent_menu,omitempty"`
}

func (s *profileService) Get(ctx context.Context, _, pageToken string) (*fbdomain.MessengerProfile, error) {
	q := url.Values{}
	q.Set("fields", profileFields)
	var out struct {
		Data []graphProfile `json:"data"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: profilePath, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return &fbdomain.MessengerProfile{}, nil
	}
	return profileOf(out.Data[0]), nil
}

func (s *profileService) Set(ctx context.Context, fbPageID, pageToken string, p fbdomain.MessengerProfile) error {
	if err := s.throttle.Allow(bucketProfile, fbPageID); err != nil {
		return err
	}
	return s.acknowledged(ctx, meta.Request{Method: http.MethodPost, Path: profilePath, Token: pageToken, Body: graphProfileOf(p), Idempotent: true})
}

func (s *profileService) Delete(ctx context.Context, fbPageID, pageToken string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	if err := s.throttle.Allow(bucketProfile, fbPageID); err != nil {
		return err
	}
	return s.acknowledged(ctx, meta.Request{Method: http.MethodDelete, Path: profilePath, Token: pageToken, Body: map[string][]string{"fields": fields}})
}

func (s *profileService) acknowledged(ctx context.Context, req meta.Request) error {
	var out struct {
		Result string `json:"result"`
	}
	if err := s.client.Do(ctx, req, &out); err != nil {
		return err
	}
	if !strings.EqualFold(out.Result, "success") {
		return fmt.Errorf("facebook: messenger profile change was not acknowledged")
	}
	return nil
}

func profileOf(g graphProfile) *fbdomain.MessengerProfile {
	out := &fbdomain.MessengerProfile{}
	for _, t := range g.Greeting {
		out.Greeting = append(out.Greeting, fbdomain.LocalizedText{Locale: t.Locale, Text: t.Text})
	}
	if g.GetStarted != nil {
		out.GetStarted = &fbdomain.GetStarted{Payload: g.GetStarted.Payload}
	}
	for _, set := range g.IceBreakers {
		items := make([]fbdomain.IceBreaker, 0, len(set.CallToActions))
		for _, c := range set.CallToActions {
			items = append(items, fbdomain.IceBreaker{Question: c.Question, Payload: c.Payload})
		}
		out.IceBreakers = append(out.IceBreakers, fbdomain.IceBreakerSet{Locale: set.Locale, Items: items})
	}
	for _, m := range g.PersistentMenu {
		items := make([]fbdomain.MenuItem, 0, len(m.CallToActions))
		for _, c := range m.CallToActions {
			items = append(items, fbdomain.MenuItem{Type: fbdomain.MenuItemType(c.Type), Title: c.Title, Payload: c.Payload, URL: c.URL})
		}
		out.PersistentMenu = append(out.PersistentMenu, fbdomain.PersistentMenu{Locale: m.Locale, ComposerInputDisabled: m.ComposerInputDisabled, Items: items})
	}
	return out
}

func graphProfileOf(p fbdomain.MessengerProfile) graphProfile {
	out := graphProfile{}
	for _, t := range p.Greeting {
		out.Greeting = append(out.Greeting, graphText{Locale: t.Locale, Text: t.Text})
	}
	if p.GetStarted != nil {
		out.GetStarted = &graphPayload{Payload: p.GetStarted.Payload}
	}
	for _, set := range p.IceBreakers {
		g := graphIceBreakers{Locale: set.Locale}
		for _, item := range set.Items {
			g.CallToActions = append(g.CallToActions, graphIceBreaker{Question: item.Question, Payload: item.Payload})
		}
		out.IceBreakers = append(out.IceBreakers, g)
	}
	for _, m := range p.PersistentMenu {
		g := graphMenu{Locale: m.Locale, ComposerInputDisabled: m.ComposerInputDisabled}
		for _, item := range m.Items {
			g.CallToActions = append(g.CallToActions, graphMenuItem{Type: string(item.Type), Title: item.Title, Payload: item.Payload, URL: item.URL})
		}
		out.PersistentMenu = append(out.PersistentMenu, g)
	}
	return out
}
