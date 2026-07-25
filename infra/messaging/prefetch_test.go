package queue

import "testing"

func TestChannelPrefetch_DefaultsToOne(t *testing.T) {
	if got := channelPrefetch("some.other.topic"); got != 1 {
		t.Fatalf("unknown topic must default to prefetch 1, got %d", got)
	}
	if got := channelPrefetch(""); got != 1 {
		t.Fatalf("empty topic must default to prefetch 1, got %d", got)
	}
}

func TestChannelPrefetch_WhatsAppMessageOverride(t *testing.T) {
	got := channelPrefetch("webhook.whatsapp.message")
	if got <= 1 {
		t.Fatalf("webhook.whatsapp.message must use prefetch > 1 for concurrency, got %d", got)
	}
}
