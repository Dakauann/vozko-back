package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
)

func TestAMessageFromAnAdCarriesTheAdIntoTheHistory(t *testing.T) {
	h := newMediaHarness(t)
	h.deliver(t, map[string]any{
		"messageid":        "msg-ad",
		"chatid":           "5511111111111@s.whatsapp.net",
		"sender":           "5511111111111@s.whatsapp.net",
		"sender_pn":        "5511111111111@s.whatsapp.net",
		"senderName":       "Maria",
		"messageType":      "ExtendedTextMessage",
		"messageTimestamp": 1786137401000,
		"text":             "Olá, vi o anúncio",
		"content": map[string]any{
			"text": "Olá, vi o anúncio",
			"contextInfo": map[string]any{"externalAdReply": map[string]any{
				"title": "Promoção de outubro", "sourceID": "120200", "sourceURL": "https://www.instagram.com/p/abc", "thumbnailURL": "https://scontent/ad.jpg",
			}},
		},
	})
	records := h.history.records
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	ad := records[0].AdReferral
	if ad == nil || ad.AdID != "120200" || ad.Title != "Promoção de outubro" || ad.Platform != conversation.AdPlatformInstagram || ad.ImageURL != "https://scontent/ad.jpg" {
		t.Fatalf("ad = %+v", ad)
	}
}
