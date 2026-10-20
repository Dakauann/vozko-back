package metawebhook

import (
	"net/http"
	"testing"
)

func TestNewRefusesAnEmptyObjectSet(t *testing.T) {
	for name, objects := range map[string][]string{
		"nil":    nil,
		"empty":  {},
		"blanks": {"", "  "},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a handler without objects would accept every object; it must fail boot")
				}
			}()
			New(Config{Name: "x", Publisher: &fakePublisher{}, Secrets: []string{secret}, Objects: objects, Route: route})
		})
	}
}

func TestOnlyTheConfiguredObjectsArePublished(t *testing.T) {
	pub := &fakePublisher{}
	h := newHandler(pub, "instagram")

	rec := post(h, []byte(`{"object":"page","entry":[{"id":"P","messaging":[{"sender":{"id":"U"},"recipient":{"id":"P"},"timestamp":1,"message":{"mid":"m"}}]}]}`))
	if rec.Code != http.StatusOK || len(pub.items) != 0 {
		t.Fatalf("code=%d published=%d, want the foreign object acked and dropped", rec.Code, len(pub.items))
	}

	rec = post(h, []byte(`{"object":"instagram","entry":[{"id":"I","messaging":[{"sender":{"id":"U"},"recipient":{"id":"I"},"timestamp":1,"message":{"mid":"m"}}]}]}`))
	if rec.Code != http.StatusOK || len(pub.items) != 1 {
		t.Fatalf("code=%d published=%d, want the configured object published", rec.Code, len(pub.items))
	}
}
