package marketing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

const activityFields = "event_type,translated_event_type,event_time,actor_name,object_id,object_name,object_type,extra_data"

type graphActivity struct {
	EventType  string       `json:"event_type"`
	Label      string       `json:"translated_event_type"`
	EventTime  string       `json:"event_time"`
	ActorName  string       `json:"actor_name"`
	ObjectID   meta.GraphID `json:"object_id"`
	ObjectName string       `json:"object_name"`
	ObjectType string       `json:"object_type"`
	ExtraData  string       `json:"extra_data"`
}

func (g *Gateway) ListActivities(ctx context.Context, token, metaAccountID string, q advertising.ActivityQuery) ([]advertising.AdActivity, error) {
	path, err := accountPath(metaAccountID)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("fields", activityFields)
	query.Set("oid", q.ObjectID)
	query.Set("add_children", "true")
	query.Set("since", strconv.FormatInt(q.Since.Unix(), 10))
	query.Set("until", strconv.FormatInt(q.Until.Unix(), 10))
	query.Set("limit", strconv.Itoa(advertising.MaxActivities))
	query.Set("locale", q.Locale)
	var page graphPage[graphActivity]
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: path + "/activities", Token: token, Query: query}, &page); err != nil {
		return nil, err
	}
	out := make([]advertising.AdActivity, 0, len(page.Data))
	for _, row := range page.Data {
		at, err := graphTime("event_time", row.EventTime)
		if err != nil {
			return nil, err
		}
		if at == nil {
			return nil, fmt.Errorf("marketing: activity %s without event_time", row.EventType)
		}
		from, to := activityChange(row.ExtraData)
		out = append(out, advertising.AdActivity{
			EventType: row.EventType, Label: row.Label, At: *at, ActorName: row.ActorName,
			ObjectID: row.ObjectID.String(), ObjectName: row.ObjectName, ObjectType: row.ObjectType, From: from, To: to,
		})
	}
	return out, nil
}

func activityChange(extra string) (string, string) {
	var change struct {
		Old any `json:"old_value"`
		New any `json:"new_value"`
	}
	if extra == "" || json.Unmarshal([]byte(extra), &change) != nil {
		return "", ""
	}
	return scalarText(change.Old), scalarText(change.New)
}

func scalarText(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	}
	return ""
}
