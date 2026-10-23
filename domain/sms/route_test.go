package sms

import (
	"errors"
	"testing"
)

func TestRouteValidate(t *testing.T) {
	ok := []Route{
		{WorkspaceID: "ws", BrandPrefix: "LojaJoao", Kind: RouteShared, Status: RouteActive},
		{WorkspaceID: "ws", BrandPrefix: "Loja do Joao", Kind: RouteDedicated, Number: "29123", Status: RouteActive},
	}
	for _, r := range ok {
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate(%+v) = %v", r, err)
		}
	}
	bad := []struct {
		name string
		r    Route
	}{
		{"no prefix", Route{WorkspaceID: "ws", Kind: RouteShared, Status: RouteActive}},
		{"prefix too long", Route{WorkspaceID: "ws", BrandPrefix: "Uma marca com nome longo demais", Kind: RouteShared, Status: RouteActive}},
		{"prefix with accents grows every sms", Route{WorkspaceID: "ws", BrandPrefix: "Promoção", Kind: RouteShared, Status: RouteActive}},
		{"prefix with symbols", Route{WorkspaceID: "ws", BrandPrefix: "Loja{1}", Kind: RouteShared, Status: RouteActive}},
		{"dedicated without number", Route{WorkspaceID: "ws", BrandPrefix: "Loja", Kind: RouteDedicated, Status: RouteActive}},
		{"shared with a number", Route{WorkspaceID: "ws", BrandPrefix: "Loja", Kind: RouteShared, Number: "29123", Status: RouteActive}},
		{"number with letters", Route{WorkspaceID: "ws", BrandPrefix: "Loja", Kind: RouteDedicated, Number: "29a23", Status: RouteActive}},
		{"unknown kind", Route{WorkspaceID: "ws", BrandPrefix: "Loja", Kind: "pigeon", Status: RouteActive}},
		{"unknown status", Route{WorkspaceID: "ws", BrandPrefix: "Loja", Kind: RouteShared, Status: "maybe"}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.r.Validate(); !errors.Is(err, ErrRouteInvalid) {
				t.Fatalf("Validate = %v", err)
			}
		})
	}
}

func TestComposeOpensEverySMSWithTheBrand(t *testing.T) {
	r := Route{BrandPrefix: "LojaJoao"}
	if got := r.Compose("Seu pedido saiu"); got != "LojaJoao: Seu pedido saiu" {
		t.Fatalf("Compose = %q", got)
	}
}

func TestFromUsesTheDedicatedNumberOrTheSharedOne(t *testing.T) {
	if got := (Route{Kind: RouteShared}).From("29000"); got != "29000" {
		t.Fatalf("shared From = %q", got)
	}
	if got := (Route{Kind: RouteDedicated, Number: "29123"}).From("29000"); got != "29123" {
		t.Fatalf("dedicated From = %q", got)
	}
}

func TestPausedRouteCannotSend(t *testing.T) {
	r := Route{Status: RouteActive}
	if !r.CanSend() {
		t.Fatal("active route sends")
	}
	r.Status = RoutePaused
	if r.CanSend() {
		t.Fatal("paused route must not send")
	}
}
