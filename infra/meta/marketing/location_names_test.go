package marketing

import (
	"context"
	"testing"

	"vozko/domain/advertising"
)

func TestDescribeLocationsReadsMetasNamesByKey(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":{"countries":{"BR":{"key":"BR","type":"country","name":"Brasil"}},"regions":{"455":{"key":"455","type":"region","name":"Rio Grande do Norte","country_code":"BR"}},"cities":{},"zips":{}}}`))
	found, err := g.DescribeLocations(context.Background(), "tok", []advertising.GeoLocation{
		{Kind: advertising.LocationCountry, Key: "BR"},
		{Kind: advertising.LocationRegion, Key: "455"},
		{Kind: advertising.LocationRegion, Key: "BR-RN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, l := range found {
		names[string(l.Kind)+":"+l.Key] = l.Name
	}
	if len(found) != 2 || names["country:BR"] != "Brasil" || names["region:455"] != "Rio Grande do Norte" {
		t.Fatalf("found %+v", found)
	}
	q := (*calls)[0].query
	if q.Get("type") != "adgeolocationmeta" || q.Get("regions") != `["455","BR-RN"]` || q.Get("countries") != `["BR"]` || q.Has("cities") {
		t.Fatalf("query %v", q)
	}
}

func TestDescribeLocationsWithNothingToAskMakesNoCall(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{}`))
	if found, err := g.DescribeLocations(context.Background(), "tok", nil); err != nil || found != nil || len(*calls) != 0 {
		t.Fatalf("found %v err %v calls %d", found, err, len(*calls))
	}
}
