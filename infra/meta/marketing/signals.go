package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var _ advertising.SignalGateway = (*Gateway)(nil)

const (
	pixelFields             = "id,name,last_fired_time,creation_time,is_unavailable"
	maxEventsPerRequest     = 1000
	actionBusinessMessaging = "business_messaging"
	actionSystemGenerated   = "system_generated"
	microsPerMajorUnit      = 1_000_000
)

type graphPixel struct {
	ID            meta.GraphID `json:"id"`
	Name          string       `json:"name"`
	LastFiredTime string       `json:"last_fired_time"`
	CreationTime  string       `json:"creation_time"`
	IsUnavailable bool         `json:"is_unavailable"`
}

func (g *Gateway) ListPixels(ctx context.Context, token, metaAccountID string) ([]advertising.Pixel, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	rows, err := collect[graphPixel](ctx, g, path+"/adspixels", token, url.Values{"fields": {pixelFields}, "limit": {"100"}})
	if err != nil {
		return nil, err
	}
	pixels := make([]advertising.Pixel, 0, len(rows))
	for _, row := range rows {
		if row.ID == "" {
			return nil, fmt.Errorf("marketing: pixel without an id")
		}
		lastFired, err := graphTime("last_fired_time", row.LastFiredTime)
		if err != nil {
			return nil, err
		}
		created, err := graphTime("creation_time", row.CreationTime)
		if err != nil {
			return nil, err
		}
		pixels = append(pixels, advertising.Pixel{
			MetaID:        row.ID.String(),
			Name:          row.Name,
			LastFiredTime: lastFired,
			CreationTime:  created,
			Unavailable:   row.IsUnavailable,
		})
	}
	return pixels, nil
}

func (g *Gateway) CreatePixel(ctx context.Context, token, metaAccountID, name string) (string, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return "", err
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/adspixels", Token: token, Form: url.Values{"name": {name}}}, "pixel")
}

func (g *Gateway) DatasetForWABA(ctx context.Context, token, wabaID string) (string, error) {
	path, err := objectPath(wabaID)
	if err != nil {
		return "", err
	}
	var existing struct {
		ID   meta.GraphID `json:"id"`
		Data []struct {
			ID meta.GraphID `json:"id"`
		} `json:"data"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/dataset", Token: token}, &existing); err != nil {
		return "", err
	}
	switch {
	case existing.ID != "":
		return existing.ID.String(), nil
	case len(existing.Data) == 1 && existing.Data[0].ID != "":
		return existing.Data[0].ID.String(), nil
	case len(existing.Data) > 1:
		return "", fmt.Errorf("marketing: whatsapp account %s has %d datasets, expected one", wabaID, len(existing.Data))
	}
	return g.created(ctx, meta.Request{Method: http.MethodPost, Path: path + "/dataset", Token: token}, "dataset")
}

type graphServerEvent struct {
	EventName        string            `json:"event_name"`
	EventTime        int64             `json:"event_time"`
	EventID          string            `json:"event_id"`
	ActionSource     string            `json:"action_source"`
	MessagingChannel string            `json:"messaging_channel,omitempty"`
	UserData         map[string]any    `json:"user_data"`
	CustomData       *graphEventValues `json:"custom_data,omitempty"`
}

type graphEventValues struct {
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

type eventDestination struct {
	target   advertising.ConversionTarget
	targetID string
}

type eventBatch struct {
	path   string
	events []graphServerEvent
}

func messagingUserData(identity advertising.MessagingIdentity) (map[string]any, error) {
	switch identity.Channel {
	case advertising.ChannelWhatsApp:
		return map[string]any{"whatsapp_business_account_id": identity.WABAID, "ctwa_clid": identity.ClickID}, nil
	case advertising.ChannelMessenger:
		return map[string]any{"page_id": identity.PageID, "page_scoped_user_id": identity.PageScopedUserID}, nil
	case advertising.ChannelInstagram:
		return map[string]any{"instagram_business_account_id": identity.InstagramUserID, "ig_sid": identity.InstagramScoped}, nil
	}
	return nil, fmt.Errorf("marketing: messaging channel %q is not supported", identity.Channel)
}

func systemUserData(event advertising.ConversionEvent) (map[string]any, error) {
	data := map[string]any{}
	if event.PhoneHash != "" {
		data["ph"] = []string{event.PhoneHash}
	}
	if event.EmailHash != "" {
		data["em"] = []string{event.EmailHash}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("marketing: event %s has no hashed phone or email", event.EventID)
	}
	return data, nil
}

func majorUnits(micros int64) json.Number {
	whole := strconv.FormatInt(micros/microsPerMajorUnit, 10)
	fraction := strings.TrimRight(fmt.Sprintf("%06d", micros%microsPerMajorUnit), "0")
	if fraction == "" {
		return json.Number(whole)
	}
	return json.Number(whole + "." + fraction)
}

func serverEvent(event advertising.ConversionEvent) (graphServerEvent, error) {
	out := graphServerEvent{
		EventName:    event.Name,
		EventTime:    event.Time.Unix(),
		EventID:      event.EventID,
		ActionSource: event.ActionSource,
	}
	var err error
	switch event.ActionSource {
	case actionBusinessMessaging:
		out.MessagingChannel = string(event.Identity.Channel)
		out.UserData, err = messagingUserData(event.Identity)
	case actionSystemGenerated:
		out.UserData, err = systemUserData(event)
	default:
		err = fmt.Errorf("marketing: action source %q is not supported", event.ActionSource)
	}
	if err != nil {
		return graphServerEvent{}, err
	}
	if event.Name == advertising.EventNamePurchase {
		if event.ValueMicros <= 0 || strings.TrimSpace(event.Currency) == "" {
			return graphServerEvent{}, fmt.Errorf("marketing: purchase %s has no value", event.EventID)
		}
		out.CustomData = &graphEventValues{Value: majorUnits(event.ValueMicros), Currency: event.Currency}
	}
	return out, nil
}

func (g *Gateway) SendEvents(ctx context.Context, token string, events []advertising.ConversionEvent) error {
	var batches []eventBatch
	index := map[eventDestination]int{}
	for _, event := range events {
		path, err := objectPath(event.TargetID)
		if err != nil {
			return err
		}
		encoded, err := serverEvent(event)
		if err != nil {
			return err
		}
		destination := eventDestination{target: event.Target, targetID: event.TargetID}
		i, seen := index[destination]
		if !seen {
			i = len(batches)
			index[destination] = i
			batches = append(batches, eventBatch{path: path})
		}
		batches[i].events = append(batches[i].events, encoded)
	}
	for _, batch := range batches {
		for _, chunk := range advertising.Batches(batch.events, maxEventsPerRequest) {
			if err := g.sendEventChunk(ctx, token, batch.path, chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *Gateway) sendEventChunk(ctx context.Context, token, path string, chunk []graphServerEvent) error {
	var out struct {
		EventsReceived *graphNumber `json:"events_received"`
	}
	body := map[string][]graphServerEvent{"data": chunk}
	if err := g.do(ctx, meta.Request{Method: http.MethodPost, Path: path + "/events", Token: token, Body: body}, &out); err != nil {
		return err
	}
	if out.EventsReceived == nil {
		return fmt.Errorf("marketing: events sent to %s were not acknowledged", path)
	}
	received, err := out.EventsReceived.wholePart("events_received")
	if err != nil {
		return err
	}
	if received != int64(len(chunk)) {
		return fmt.Errorf("marketing: meta received %d of %d events sent to %s", received, len(chunk), path)
	}
	return nil
}
