package carriermessaging

import (
	"net/http"
	"testing"
)

func TestAdminPauseRefusesUnknownReasons(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).AdminPauseSender, http.MethodPost, "/admin/carrier/senders/s1/pause", "/admin/carrier/senders/{id}/pause", AdminPauseRequest{Reason: "bored"})
	if rec.Code != http.StatusUnprocessableEntity || decodeError(t, rec)["code"] != "rcs_pause_reason_invalid" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestAdminAssignNumberRefusesATakenNumber(t *testing.T) {
	rec := call(t, NewHandler(&fakeAPI{}).AdminAssignNumber, http.MethodPut, "/admin/carrier/sms/routes/r1/number", "/admin/carrier/sms/routes/{id}/number", RouteNumberRequest{Number: "29123"})
	if rec.Code != http.StatusConflict || decodeError(t, rec)["code"] != "sms_number_taken" {
		t.Fatalf("code = %d, body = %s", rec.Code, rec.Body)
	}
}
