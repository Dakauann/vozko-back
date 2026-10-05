package pricing_analytics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	analytics_domain "vozko/domain/analytics"
	"vozko/infra/meta"
)

func gatewayAgainst(t *testing.T, handler http.HandlerFunc) *Gateway {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := meta.NewClient(meta.Config{Host: strings.TrimPrefix(server.URL, "https://"), APIVersion: "v25.0", MaxRetries: 1, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return NewGateway(client)
}

func TestVolumesAreReadDailyByCategoryAndPricingType(t *testing.T) {
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	var fields string
	gateway := gatewayAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v25.0/1029" {
			t.Errorf("path %s", r.URL.Path)
		}
		fields = r.URL.Query().Get("fields")
		_, _ = w.Write([]byte(`{"id":"1029","pricing_analytics":{"data":[{"data_points":[
			{"start":1790218800,"end":1790305200,"pricing_type":"REGULAR","pricing_category":"UTILITY","volume":10201},
			{"start":1790218800,"end":1790305200,"pricing_type":"FREE_CUSTOMER_SERVICE","pricing_category":"SERVICE","volume":5041}
		]}]}}`))
	})

	points, err := gateway.Volumes(context.Background(), "1029", "tok", start, end)
	if err != nil {
		t.Fatal(err)
	}
	want := []analytics_domain.MetaVolumePoint{
		{Category: "UTILITY", PricingType: "REGULAR", Volume: 10201},
		{Category: "SERVICE", PricingType: "FREE_CUSTOMER_SERVICE", Volume: 5041},
	}
	if len(points) != len(want) || points[0] != want[0] || points[1] != want[1] {
		t.Fatalf("points %+v", points)
	}
	wantFields := `pricing_analytics.start(1790823600).end(1793502000).granularity(DAILY).metric_types(["VOLUME"]).dimensions(["PRICING_CATEGORY","PRICING_TYPE"])`
	if fields != wantFields {
		t.Fatalf("fields %q, want %q", fields, wantFields)
	}
}

func TestAPeriodWithoutTrafficHasNoPoints(t *testing.T) {
	gateway := gatewayAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1029"}`))
	})
	points, err := gateway.Volumes(context.Background(), "1029", "tok", time.Now(), time.Now())
	if err != nil || len(points) != 0 {
		t.Fatalf("points %+v err %v", points, err)
	}
}

func TestMetaRefusingTheReadIsAnError(t *testing.T) {
	gateway := gatewayAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"Application does not have permission for this action","code":10,"error_subcode":2388184}}`))
	})
	if _, err := gateway.Volumes(context.Background(), "1029", "tok", time.Now(), time.Now()); err == nil {
		t.Fatal("a refused read must not look like zero messages")
	}
}
