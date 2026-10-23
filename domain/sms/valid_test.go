package sms

import "testing"

func TestRouteKindAndStatusValidity(t *testing.T) {
	for _, k := range []RouteKind{RouteShared, RouteDedicated} {
		if !k.Valid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	for _, s := range []RouteStatus{RouteActive, RoutePaused} {
		if !s.Valid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if RouteKind("pigeon").Valid() || RouteStatus("maybe").Valid() {
		t.Fatal("unknown values are invalid")
	}
}
