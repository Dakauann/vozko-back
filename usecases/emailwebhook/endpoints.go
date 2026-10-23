package emailwebhook_usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"vozko/domain/emailapi"
	wh "vozko/domain/emailwebhook"
)

type CreateInput struct {
	WorkspaceID string
	UserID      string
	URL         string
	Events      []string
	Enabled     bool
}

type WithSecret struct {
	Endpoint wh.Endpoint
	Secret   string
}

func (m *Module) checkURL(ctx context.Context, raw string) (string, error) {
	url, err := wh.ValidateURL(raw)
	if err != nil {
		return "", err
	}
	if err := m.d.Hosts.CheckHost(ctx, wh.HostOf(url)); err != nil {
		return "", fmt.Errorf("%w: %v", wh.ErrURLBlocked, err)
	}
	return url, nil
}

func (m *Module) newSecret() (string, []byte, error) {
	secret, err := wh.GenerateSecret(m.d.Random)
	if err != nil {
		return "", nil, err
	}
	sealed, err := m.d.Cipher.Encrypt([]byte(secret))
	if err != nil {
		return "", nil, fmt.Errorf("emailwebhook: sealing the secret: %w", err)
	}
	return secret, sealed, nil
}

func (m *Module) Create(ctx context.Context, in CreateInput) (WithSecret, error) {
	url, err := m.checkURL(ctx, in.URL)
	if err != nil {
		return WithSecret{}, err
	}
	events, err := wh.ValidateEvents(in.Events)
	if err != nil {
		return WithSecret{}, err
	}
	secret, sealed, err := m.newSecret()
	if err != nil {
		return WithSecret{}, err
	}
	now := m.d.Now()
	e := wh.Endpoint{ID: m.d.NewID(), WorkspaceID: in.WorkspaceID, URL: url, Events: events, Enabled: in.Enabled,
		SecretCipher: sealed, SecretHint: wh.Hint(secret), CreatedBy: in.UserID, CreatedAt: now, UpdatedAt: now}
	if err := m.d.Endpoints.Create(ctx, &e, wh.MaxEndpointsPerWorkspace); err != nil {
		return WithSecret{}, err
	}
	return WithSecret{Endpoint: e, Secret: secret}, nil
}

type UpdateInput struct {
	WorkspaceID string
	ID          string
	URL         *string
	Events      []string
	Enabled     *bool
}

func (m *Module) Update(ctx context.Context, in UpdateInput) (wh.Endpoint, error) {
	e, err := m.d.Endpoints.Get(ctx, in.WorkspaceID, in.ID)
	if err != nil {
		return wh.Endpoint{}, err
	}
	if in.URL != nil {
		url, err := m.checkURL(ctx, *in.URL)
		if err != nil {
			return wh.Endpoint{}, err
		}
		e.URL = url
	}
	if in.Events != nil {
		events, err := wh.ValidateEvents(in.Events)
		if err != nil {
			return wh.Endpoint{}, err
		}
		e.Events = events
	}
	if in.Enabled != nil {
		e.Enabled = *in.Enabled
	}
	e.UpdatedAt = m.d.Now()
	if err := m.d.Endpoints.Update(ctx, e); err != nil {
		return wh.Endpoint{}, err
	}
	if !e.Enabled {
		m.resolve(ctx, *e)
	}
	return *e, nil
}

func (m *Module) Delete(ctx context.Context, workspaceID, id string) error {
	e, err := m.d.Endpoints.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if err := m.d.Endpoints.Delete(ctx, workspaceID, id); err != nil {
		return err
	}
	m.resolve(ctx, *e)
	return nil
}

func (m *Module) RotateSecret(ctx context.Context, workspaceID, id string) (WithSecret, error) {
	e, err := m.d.Endpoints.Get(ctx, workspaceID, id)
	if err != nil {
		return WithSecret{}, err
	}
	secret, sealed, err := m.newSecret()
	if err != nil {
		return WithSecret{}, err
	}
	if err := m.d.Endpoints.SetSecret(ctx, workspaceID, id, sealed, wh.Hint(secret), m.d.Now()); err != nil {
		return WithSecret{}, err
	}
	e.SecretCipher, e.SecretHint = sealed, wh.Hint(secret)
	return WithSecret{Endpoint: *e, Secret: secret}, nil
}

func (m *Module) List(ctx context.Context, workspaceID string) ([]wh.Endpoint, error) {
	return m.d.Endpoints.List(ctx, workspaceID)
}

type DeliveryView struct {
	Delivery wh.Delivery
	Attempts []wh.Attempt
}

type DeliveryPage struct {
	Items []DeliveryView
	Next  string
}

func (m *Module) Deliveries(ctx context.Context, workspaceID, endpointID, cursor string, limit int) (DeliveryPage, error) {
	if _, err := m.d.Endpoints.Get(ctx, workspaceID, endpointID); err != nil {
		return DeliveryPage{}, err
	}
	c, err := emailapi.ParseCursor(cursor)
	if err != nil {
		return DeliveryPage{}, err
	}
	if limit <= 0 || limit > emailapi.MaxPageSize {
		limit = emailapi.DefaultPageSize
	}
	rows, err := m.d.Deliveries.ListByEndpoint(ctx, workspaceID, endpointID, c, limit)
	if err != nil {
		return DeliveryPage{}, err
	}
	page := emailapi.PageOf(rows, limit)
	ids := make([]string, len(page.Items))
	for i, d := range page.Items {
		ids[i] = d.ID
	}
	attempts, err := m.d.Deliveries.Attempts(ctx, ids)
	if err != nil {
		return DeliveryPage{}, err
	}
	out := DeliveryPage{Next: page.Next, Items: make([]DeliveryView, len(page.Items))}
	for i, d := range page.Items {
		out.Items[i] = DeliveryView{Delivery: d, Attempts: attempts[d.ID]}
	}
	return out, nil
}

func (m *Module) Retry(ctx context.Context, workspaceID, endpointID, deliveryID string) (wh.Delivery, error) {
	d, err := m.d.Deliveries.Retry(ctx, workspaceID, endpointID, deliveryID, m.d.Now())
	if err != nil {
		return wh.Delivery{}, err
	}
	if err := m.publishDelivery(DeliveryMessage{DeliveryID: d.ID, EndpointID: d.EndpointID, Attempt: d.Attempts + 1}); err != nil {
		m.d.Logf("[email-webhook] retry of delivery %s is queued in the database and goes out with the next sweep: %v", d.ID, err)
	}
	return *d, nil
}

func (m *Module) publishDelivery(msg DeliveryMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return m.d.Queue.Publish(TopicDelivery, payload)
}
