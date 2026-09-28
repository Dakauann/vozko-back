package facebook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type GraphConfig struct {
	GraphVersion string
	AppSecret    string
	HTTPClient   *http.Client
}

func newGraphClient(cfg GraphConfig, host string) (*meta.Client, error) {
	return meta.NewClient(meta.Config{
		Host:       host,
		APIVersion: meta.VersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
}

type subscriptionService struct {
	client *meta.Client
}

func NewSubscriptionService(cfg GraphConfig) (fbdomain.SubscriptionService, error) {
	client, err := newGraphClient(cfg, GraphHost)
	if err != nil {
		return nil, err
	}
	return &subscriptionService{client: client}, nil
}

type subscribeResponse struct {
	Success          bool  `json:"success"`
	MessagingSuccess *bool `json:"messaging_success"`
}

func (s *subscriptionService) Subscribe(ctx context.Context, fbPageID, pageToken string, fields []string) ([]string, error) {
	if bad := fbdomain.InvalidSubscribedFields(fields); len(bad) > 0 {
		return nil, fmt.Errorf("facebook: refusing to subscribe, invalid webhook field(s) %v", bad)
	}
	q := url.Values{}
	q.Set("subscribed_fields", strings.Join(fields, ","))
	var out subscribeResponse
	if err := s.client.Do(ctx, meta.Request{
		Method:     http.MethodPost,
		Path:       "/" + fbPageID + "/subscribed_apps",
		Token:      pageToken,
		Query:      q,
		Idempotent: true,
	}, &out); err != nil {
		return nil, err
	}
	if !out.Success {
		return nil, fmt.Errorf("facebook: webhook subscription was not acknowledged for page %s", fbPageID)
	}
	return s.ActiveFields(ctx, fbPageID, pageToken)
}

func (s *subscriptionService) ActiveFields(ctx context.Context, fbPageID, pageToken string) ([]string, error) {
	var out struct {
		Data []struct {
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + fbPageID + "/subscribed_apps",
		Token:  pageToken,
	}, &out); err != nil {
		return nil, err
	}
	var active []string
	for _, app := range out.Data {
		active = append(active, app.SubscribedFields...)
	}
	return active, nil
}

func (s *subscriptionService) Unsubscribe(ctx context.Context, fbPageID, pageToken string) error {
	var out subscribeResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodDelete,
		Path:   "/" + fbPageID + "/subscribed_apps",
		Token:  pageToken,
	}, &out); err != nil {
		return err
	}
	if !out.Success || (out.MessagingSuccess != nil && !*out.MessagingSuccess) {
		return fmt.Errorf("facebook: webhook unsubscribe was only partially applied for page %s", fbPageID)
	}
	return nil
}

func NewMediaFetcher(cfg GraphConfig) (*meta.Client, error) {
	return newGraphClient(cfg, GraphHost)
}
