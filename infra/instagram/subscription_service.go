package instagram

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	igdomain "vozko/domain/instagram"
	"vozko/infra/meta"
)

type subscriptionService struct {
	client *meta.Client
}

func NewSubscriptionService(cfg GraphConfig) (igdomain.SubscriptionService, error) {
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: graphVersionOr(cfg.GraphVersion),
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return &subscriptionService{client: client}, nil
}

type subscribeResponse struct {
	Success          bool  `json:"success"`
	MessagingSuccess *bool `json:"messaging_success"`
}

func (s *subscriptionService) Subscribe(ctx context.Context, igUserID, token string, fields []string) error {
	if len(fields) == 0 {
		fields = igdomain.SubscribedFields()
	}

	if bad := igdomain.InvalidSubscribedFields(fields); len(bad) > 0 {
		return fmt.Errorf("instagram: refusing to subscribe, invalid webhook field(s) %v "+
			"(a single bad entry voids the entire subscription)", bad)
	}

	q := url.Values{}
	q.Set("subscribed_fields", strings.Join(fields, ","))

	var out subscribeResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodPost,
		Path:   "/" + igUserID + "/subscribed_apps",
		Token:  token,
		Query:  q,
	}, &out); err != nil {
		return err
	}
	if !out.Success {
		return fmt.Errorf("instagram: webhook subscription was not acknowledged for account %s", igUserID)
	}

	if active, err := s.activeFields(ctx, igUserID, token); err != nil {
		log.Printf("[instagram] subscribed account=%s but could not verify fields: %v", igUserID, err)
	} else {
		missing := difference(fields, active)
		if len(missing) > 0 {
			log.Printf("[instagram] account=%s subscribed, but these fields are NOT active: %v (active: %v)",
				igUserID, missing, active)
		} else {
			log.Printf("[instagram] account=%s subscription verified, %d field(s) active", igUserID, len(active))
		}
	}
	return nil
}

func (s *subscriptionService) activeFields(ctx context.Context, igUserID, token string) ([]string, error) {
	var out struct {
		Data []struct {
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodGet,
		Path:   "/" + igUserID + "/subscribed_apps",
		Token:  token,
	}, &out); err != nil {
		return nil, err
	}

	var active []string
	for _, app := range out.Data {
		active = append(active, app.SubscribedFields...)
	}
	return active, nil
}

func difference(want, got []string) []string {
	have := make(map[string]struct{}, len(got))
	for _, g := range got {
		have[g] = struct{}{}
	}
	var missing []string
	for _, w := range want {
		if _, ok := have[w]; !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

func (s *subscriptionService) Unsubscribe(ctx context.Context, igUserID, token string) error {
	var out subscribeResponse
	if err := s.client.Do(ctx, meta.Request{
		Method: http.MethodDelete,
		Path:   "/" + igUserID + "/subscribed_apps",
		Token:  token,
	}, &out); err != nil {
		return err
	}
	if !out.Success || (out.MessagingSuccess != nil && !*out.MessagingSuccess) {
		return fmt.Errorf("instagram: webhook unsubscribe was only partially applied for account %s", igUserID)
	}
	return nil
}
