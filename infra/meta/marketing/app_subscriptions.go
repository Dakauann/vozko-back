package marketing

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var errAppCredentialsMissing = errors.New("marketing: the ads app id and secret are required to manage webhook subscriptions")

type graphAppSubscription struct {
	Object      string `json:"object"`
	CallbackURL string `json:"callback_url"`
	Active      bool   `json:"active"`
	Fields      []struct {
		Name string `json:"name"`
	} `json:"fields"`
}

func (g *Gateway) appToken() (string, error) {
	if strings.TrimSpace(g.appID) == "" || strings.TrimSpace(g.appSecret) == "" {
		return "", errAppCredentialsMissing
	}
	return g.appID + "|" + g.appSecret, nil
}

func (g *Gateway) AppSubscriptions(ctx context.Context) ([]advertising.AppSubscription, error) {
	token, err := g.appToken()
	if err != nil {
		return nil, err
	}
	var page graphPage[graphAppSubscription]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + g.appID + "/subscriptions", Token: token}, &page); err != nil {
		return nil, err
	}
	out := make([]advertising.AppSubscription, 0, len(page.Data))
	for _, row := range page.Data {
		sub := advertising.AppSubscription{Object: row.Object, CallbackURL: row.CallbackURL, Active: row.Active}
		for _, field := range row.Fields {
			sub.Fields = append(sub.Fields, field.Name)
		}
		out = append(out, sub)
	}
	return out, nil
}

func (g *Gateway) SubscribeApp(ctx context.Context, sub advertising.AppSubscription, verifyToken string) error {
	token, err := g.appToken()
	if err != nil {
		return err
	}
	form := url.Values{
		"object":       {sub.Object},
		"callback_url": {sub.CallbackURL},
		"fields":       {strings.Join(sub.Fields, ",")},
		"verify_token": {verifyToken},
	}
	var out struct {
		Success bool `json:"success"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + g.appID + "/subscriptions", Token: token, Form: form, Idempotent: true}, &out); err != nil {
		return err
	}
	if !out.Success {
		return advertising.ErrWebhookNotSubscribed
	}
	return nil
}
