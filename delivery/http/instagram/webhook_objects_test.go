package instagram

import (
	"net/http"
	"testing"
)

func TestReceive_IgnoresObjectsThatAreNotInstagram(t *testing.T) {
	pub := &fakePublisher{}
	body := []byte(`{"object":"page","entry":[{"id":"P","time":1,"messaging":[{"sender":{"id":"U"},"recipient":{"id":"P"},"timestamp":1,"message":{"mid":"m","text":"oi"}}]}]}`)

	rec := postWebhook(t, newTestHandler(pub), body, sign(testIGSecret, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(pub.Published) != 0 {
		t.Fatalf("published %d entries for a page object, want none", len(pub.Published))
	}
}
