package lead

import (
	"math"
	"net/http"
	"testing"

	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
)

func TestCreateCarriesThePinChosenBeforeSaving(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 1}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads", nil, map[string]any{
		"name":      "Maria",
		"addresses": []map[string]any{{"label": "home", "zipCode": "50030230", "pin": map[string]any{"latitude": -8.0476, "longitude": -34.877}}},
	})
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	pin := cmds.draft.Addresses[0].Pin
	if pin == nil || *pin != (geo.Point{Lat: -8.0476, Lng: -34.877}) {
		t.Fatalf("pin = %+v, want the chosen point", pin)
	}
}

func TestUpdateCarriesThePinAndLeavesItOutWhenNotSent(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{
		"addresses": []map[string]any{
			{"id": "a-1", "label": "home", "primary": true, "zipCode": "01310100", "pin": map[string]any{"latitude": -23.56, "longitude": -46.65}},
			{"label": "work", "zipCode": "01310100"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	addresses := *cmds.edit.Addresses
	if addresses[0].Pin == nil || *addresses[0].Pin != (geo.Point{Lat: -23.56, Lng: -46.65}) || addresses[1].Pin != nil {
		t.Fatalf("addresses = %+v, want the pin only on the first", addresses)
	}
}

func TestAPinWithoutBothCoordinatesNeverBecomesAPoint(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
	send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{
		"addresses": []map[string]any{{"id": "a-1", "label": "home", "zipCode": "01310100", "pin": map[string]any{"latitude": -23.56}}},
	})
	if cmds.edit.Addresses == nil {
		t.Fatal("the edit never reached the use case")
	}
	pin := (*cmds.edit.Addresses)[0].Pin
	if pin == nil || !math.IsNaN(pin.Lng) || pin.Validate() == nil {
		t.Fatalf("pin = %+v, want a point the domain refuses", pin)
	}
}
