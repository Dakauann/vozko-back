package marketing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"vozko/domain/advertising"
	"vozko/infra/meta"
)

var geoMetaParams = map[advertising.LocationKind]string{
	advertising.LocationCountry: "countries",
	advertising.LocationRegion:  "regions",
	advertising.LocationCity:    "cities",
}

type graphGeoMeta struct {
	Key  meta.GraphID `json:"key"`
	Name string       `json:"name"`
}

func (g *Gateway) DescribeLocations(ctx context.Context, token string, locations []advertising.GeoLocation) ([]advertising.RemoteLocation, error) {
	keys := map[advertising.LocationKind][]string{}
	for _, l := range locations {
		if _, supported := geoMetaParams[l.Kind]; supported {
			keys[l.Kind] = append(keys[l.Kind], l.Key)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	q := url.Values{}
	q.Set("type", "adgeolocationmeta")
	q.Set("locale", searchLocale)
	for kind, list := range keys {
		raw, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		q.Set(geoMetaParams[kind], string(raw))
	}
	var out struct {
		Data map[string]map[string]graphGeoMeta `json:"data"`
	}
	if err := g.do(ctx, meta.Request{Method: http.MethodGet, Path: "/search", Token: token, Query: q}, &out); err != nil {
		return nil, err
	}
	var found []advertising.RemoteLocation
	for kind, param := range geoMetaParams {
		for _, row := range out.Data[param] {
			found = append(found, advertising.RemoteLocation{Kind: kind, Key: row.Key.String(), Name: row.Name})
		}
	}
	return found, nil
}
