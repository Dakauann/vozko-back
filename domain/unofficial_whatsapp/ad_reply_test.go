package unofficial_whatsapp

import (
	"encoding/base64"
	"strconv"
	"testing"
)

const adChat = `"chatid":"5511999999999@s.whatsapp.net","messageid":"AD1","isGroup":false,"fromMe":false,"sender":"5511999999999@s.whatsapp.net","messageTimestamp":1786137401000`

func adEvent(t *testing.T, content string) *Event {
	t.Helper()
	body := `{"EventType":"messages","message":{` + adChat + `,"messageType":"ExtendedTextMessage","text":"Olá, vi o anúncio","content":` + content + `}}`
	env, err := DecodeEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	evs := NormalizeEnvelope("inst-1", env)
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	return evs[0]
}

const adReplyJSON = `{"title":"Promoção de outubro","body":"Fale com a gente","sourceType":"ad","sourceID":"120200","sourceURL":"https://fb.me/abc","thumbnailURL":"https://scontent/ad.jpg","ctwaClid":"x"}`

func TestAnAdClickOnAWhatsAppWebNumberIsRead(t *testing.T) {
	cases := map[string]string{
		"content object":                `{"text":"Olá, vi o anúncio","contextInfo":{"externalAdReply":` + adReplyJSON + `}}`,
		"content serialized as text":    strconv.Quote(`{"text":"Olá, vi o anúncio","contextInfo":{"externalAdReply":` + adReplyJSON + `}}`),
		"nested extendedTextMessage":    `{"extendedTextMessage":{"text":"Olá","contextInfo":{"externalAdReply":` + adReplyJSON + `}}}`,
		"lower camel thumbnail spelling": `{"text":"Olá","contextInfo":{"externalAdReply":{"title":"Promoção de outubro","sourceId":"120200","sourceUrl":"https://fb.me/abc","thumbnailUrl":"https://scontent/ad.jpg"}}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			ev := adEvent(t, content)
			if ev.AdReply == nil || ev.AdReply.Title != "Promoção de outubro" || ev.AdReply.SourceID != "120200" ||
				ev.AdReply.SourceURL != "https://fb.me/abc" || ev.AdReply.ThumbnailURL != "https://scontent/ad.jpg" {
				t.Fatalf("ad = %+v", ev.AdReply)
			}
			if ev.Text == "" {
				t.Fatal("the message text must survive")
			}
		})
	}
}

func TestAnInlineAdThumbnailIsDecoded(t *testing.T) {
	thumb := base64.StdEncoding.EncodeToString([]byte("jpeg-bytes"))
	ev := adEvent(t, `{"text":"Olá","contextInfo":{"externalAdReply":{"title":"Anúncio","thumbnail":"`+thumb+`"}}}`)
	if ev.AdReply == nil || string(ev.AdReply.Thumbnail) != "jpeg-bytes" {
		t.Fatalf("ad = %+v", ev.AdReply)
	}
}

func TestAnOrdinaryMessageHasNoAd(t *testing.T) {
	for _, content := range []string{`"bom dia"`, `{"text":"bom dia","contextInfo":{"forwardingScore":2}}`, `{"text":"x","contextInfo":{"externalAdReply":{}}}`} {
		if ev := adEvent(t, content); ev.AdReply != nil {
			t.Fatalf("content %s: ad = %+v", content, ev.AdReply)
		}
	}
}
