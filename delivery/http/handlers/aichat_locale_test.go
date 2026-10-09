package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestTheEloContextCarriesTheLocaleOfTheRequest(t *testing.T) {
	r := httptest.NewRequest("POST", "/chat/threads/t-1/messages", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if cc := copilotCtx(r, "u-1", "ws-1"); cc.Locale != "en" {
		t.Fatalf("locale = %q", cc.Locale)
	}
}
