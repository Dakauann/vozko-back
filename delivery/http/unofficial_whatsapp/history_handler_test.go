package unofficial_whatsapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHistorySyncEndpointsAnswerWhenTheImportIsSwitchedOff(t *testing.T) {
	h := &Handler{}
	for _, handle := range []http.HandlerFunc{h.GetHistorySync, h.RequestHistorySync} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/instances/i-1/history-sync", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503 when the import is disabled", rec.Code)
		}
	}
}
