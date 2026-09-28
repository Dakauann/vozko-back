package facebook

import (
	"context"
	"net/http"
	"net/url"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

type routingService struct {
	client *meta.Client
}

func NewRoutingService(cfg GraphConfig) (fbdomain.RoutingService, error) {
	client, err := newGraphClient(cfg, GraphHost)
	if err != nil {
		return nil, err
	}
	return &routingService{client: client}, nil
}

type threadOwnerResponse struct {
	Data []struct {
		ThreadOwner struct {
			AppID meta.GraphID `json:"app_id"`
		} `json:"thread_owner"`
	} `json:"data"`
}

func (s *routingService) ThreadOwner(ctx context.Context, fbPageID, pageToken, psid string) (string, error) {
	q := url.Values{}
	q.Set("recipient", psid)
	var out threadOwnerResponse
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPageID + "/thread_owner", Token: pageToken, Query: q}, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", nil
	}
	return out.Data[0].ThreadOwner.AppID.String(), nil
}

func (s *routingService) TakeControl(ctx context.Context, fbPageID, pageToken, psid, metadata string) error {
	return s.control(ctx, fbPageID+"/take_thread_control", pageToken, map[string]any{
		"recipient": map[string]string{"id": psid},
		"metadata":  metadata,
	})
}

func (s *routingService) ReleaseControl(ctx context.Context, fbPageID, pageToken, psid string) error {
	return s.control(ctx, fbPageID+"/release_thread_control", pageToken, map[string]any{
		"recipient": map[string]string{"id": psid},
	})
}

func (s *routingService) control(ctx context.Context, path, pageToken string, body map[string]any) error {
	var out struct {
		Success bool `json:"success"`
	}
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodPost, Path: "/" + path, Token: pageToken, Body: body, Idempotent: true}, &out); err != nil {
		return err
	}
	if !out.Success {
		return &meta.Error{Message: "thread control request was not acknowledged"}
	}
	return nil
}
