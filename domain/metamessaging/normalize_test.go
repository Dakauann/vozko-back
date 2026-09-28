package metamessaging

import (
	"testing"
	"time"
)

var messenger = Dialect{Prefix: "fb", ClassifyStandby: true}
var instagram = Dialect{Prefix: "ig"}

func normalizeOne(t *testing.T, d Dialect, body string) []*Event {
	t.Helper()
	envs, err := DecodeEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	entries := SplitEntries(envs)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	return NormalizeMessaging(entries[0], d)
}

func single(t *testing.T, events []*Event) *Event {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	return events[0]
}

func TestDecodeEnvelopeAcceptsArrayAndObject(t *testing.T) {
	for _, body := range []string{
		`{"object":"page","entry":[{"id":"1","messaging":[]}]}`,
		`[{"object":"page","entry":[{"id":"1","messaging":[]}]}]`,
	} {
		envs, err := DecodeEnvelope([]byte(body))
		if err != nil || len(envs) != 1 || envs[0].Object != "page" {
			t.Fatalf("%s: %v %+v", body, err, envs)
		}
	}
	for _, body := range []string{"", "  ", "nope", `{"entry":`} {
		if _, err := DecodeEnvelope([]byte(body)); err == nil {
			t.Fatalf("%q accepted", body)
		}
	}
}

func TestMessengerInboundText(t *testing.T) {
	ev := single(t, normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1458692752478,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":1458692752478,
		 "message":{"mid":"m_1","text":"hello","quick_reply":{"payload":"opt:1"}}}]}]}`))
	if ev.Kind != EventInboundMessage || ev.AccountExternalID != "PAGE" || ev.ContactExternalID != "PSID" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Message.QuickReply == nil || ev.Message.QuickReply.Payload != "opt:1" {
		t.Fatalf("quick reply lost: %+v", ev.Message)
	}
	if ev.IdempotencyKey != "fb:PAGE:messages:m_1" {
		t.Fatalf("key = %s", ev.IdempotencyKey)
	}
	if !ev.Timestamp.Equal(time.UnixMilli(1458692752478).UTC()) {
		t.Fatalf("timestamp = %v", ev.Timestamp)
	}
}

func TestMessengerEchoCarriesAppIDAndMetadata(t *testing.T) {
	ev := single(t, normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PAGE"},"recipient":{"id":"PSID"},"timestamp":1457764197627,
		 "message":{"is_echo":true,"app_id":1517776481860111,"metadata":"vozko:ai","mid":"m_2","text":"hi"}}]}]}`))
	if ev.Kind != EventEchoMessage || ev.ContactExternalID != "PSID" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Message.AppID.String() != "1517776481860111" || ev.Message.Metadata != "vozko:ai" {
		t.Fatalf("echo attribution lost: %+v", ev.Message)
	}
}

func TestMessengerStandbyIsClassifiedAndFlagged(t *testing.T) {
	ev := single(t, normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"standby":[
		{"sender":{"id":"PAGE"},"recipient":{"id":"PSID"},"timestamp":1570053170673,
		 "message":{"mid":"m_3","is_echo":true,"app_id":263902037430900,"text":"from inbox"}}]}]}`))
	if ev.Kind != EventEchoMessage || !ev.Standby {
		t.Fatalf("event = %+v", ev)
	}
	if ev.IdempotencyKey != "fb:PAGE:messages:m_3" {
		t.Fatalf("key = %s", ev.IdempotencyKey)
	}
}

func TestInstagramStandbyKeepsTheOpaqueKind(t *testing.T) {
	ev := single(t, normalizeOne(t, instagram, `{"object":"instagram","entry":[{"id":"IG","time":1,"standby":[
		{"sender":{"id":"U"},"recipient":{"id":"IG"},"timestamp":1,"message":{"mid":"m_4","text":"x"}}]}]}`))
	if ev.Kind != EventStandby || !ev.Standby {
		t.Fatalf("event = %+v", ev)
	}
}

func TestMessengerDeliveryAndReadAreWatermarks(t *testing.T) {
	events := normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":1458668856300,
		 "delivery":{"mids":["m_1"],"watermark":1458668856253}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":1458668856463,
		 "read":{"watermark":1458668856253}}]}]}`)
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	delivery, read := events[0], events[1]
	if delivery.Kind != EventDelivery || delivery.Delivery.WatermarkTime().IsZero() {
		t.Fatalf("delivery = %+v", delivery)
	}
	if read.Kind != EventRead || !read.Read.WatermarkTime().Equal(time.UnixMilli(1458668856253).UTC()) {
		t.Fatalf("read = %+v", read)
	}
	if read.IdempotencyKey != "fb:PAGE:read:w1458668856253" {
		t.Fatalf("read key = %s", read.IdempotencyKey)
	}
}

func TestDeliveryWithoutMidsStillNormalizes(t *testing.T) {
	ev := single(t, normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":5,"delivery":{"watermark":4}}]}]}`))
	if ev.Kind != EventDelivery || len(ev.Delivery.MIDs) != 0 {
		t.Fatalf("event = %+v", ev)
	}
}

func TestStickerIDAndFallbackWithoutPayload(t *testing.T) {
	ev := single(t, normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":5,
		 "message":{"mid":"m_5","attachments":[{"type":"sticker","payload":{"url":"https://s","sticker_id":369239263222822}},{"type":"fallback"}]}}]}]}`))
	atts := ev.Message.Attachments
	if len(atts) != 2 || atts[0].Payload.StickerID.String() != "369239263222822" || atts[1].Payload != nil {
		t.Fatalf("attachments = %+v", atts)
	}
}

func TestThreadControlPolicyAndOptinEvents(t *testing.T) {
	events := normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":10,
		 "take_thread_control":{"previous_owner_app_id":"111","new_owner_app_id":"263902037430900"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":11,
		 "pass_thread_control":{"previous_owner_app_id":null,"new_owner_app_id":"111"}},
		{"recipient":{"id":"PAGE"},"timestamp":12,"policy_enforcement":{"action":"block","reason":"spam"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":13,"optin":{"type":"notification_messages","notification_messages_status":"STOP"}}]}]}`)
	want := []EventKind{EventThreadControl, EventThreadControl, EventPolicyEnforcement, EventOptin}
	if len(events) != len(want) {
		t.Fatalf("events = %d", len(events))
	}
	for i, k := range want {
		if events[i].Kind != k {
			t.Fatalf("event %d kind = %s, want %s", i, events[i].Kind, k)
		}
	}
	if events[0].ThreadControl.NewOwnerAppID.String() != "263902037430900" {
		t.Fatalf("take = %+v", events[0].ThreadControl)
	}
	if events[1].ThreadControl.NewOwnerAppID.String() != "111" || events[1].ThreadControl.PreviousOwnerAppID.String() != "" {
		t.Fatalf("pass = %+v", events[1].ThreadControl)
	}
	if events[2].Policy.Action != "block" || events[2].AccountExternalID != "PAGE" {
		t.Fatalf("policy = %+v", events[2])
	}
	if events[0].IdempotencyKey == events[1].IdempotencyKey {
		t.Fatal("thread control keys collide")
	}
}

func TestReactionEditAndPostbackKeysDoNotCollide(t *testing.T) {
	events := normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":20,"reaction":{"mid":"m_1","action":"react","reaction":"love","emoji":"x"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":21,"message_edit":{"mid":"m_1","text":"new","num_edit":"2"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":22,"postback":{"mid":"m_1","title":"Go","payload":"GET_STARTED"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":23,"referral":{"ref":"promo","source":"ADS","type":"OPEN_THREAD","ad_id":"9","ads_context_data":{"ad_title":"Sale"}}}]}]}`)
	seen := map[string]bool{}
	for _, ev := range events {
		if seen[ev.IdempotencyKey] {
			t.Fatalf("duplicate key %s", ev.IdempotencyKey)
		}
		seen[ev.IdempotencyKey] = true
	}
	if events[3].Referral.AdsContextData == nil || events[3].Referral.AdsContextData.AdTitle != "Sale" {
		t.Fatalf("referral = %+v", events[3].Referral)
	}
}

func TestEventsAreSortedByTimestamp(t *testing.T) {
	events := normalizeOne(t, messenger, `{"object":"page","entry":[{"id":"PAGE","time":1,"messaging":[
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":30,"message":{"mid":"m_b","text":"b"}},
		{"sender":{"id":"PSID"},"recipient":{"id":"PAGE"},"timestamp":10,"message":{"mid":"m_a","text":"a"}}]}]}`)
	if events[0].Message.MID != "m_a" {
		t.Fatalf("not sorted: %s first", events[0].Message.MID)
	}
}

func TestUnixTimeAcceptsSecondsAndMilliseconds(t *testing.T) {
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, v := range []int64{want.Unix(), want.UnixMilli()} {
		if got := UnixTime(v); !got.Equal(want) {
			t.Fatalf("UnixTime(%d) = %v", v, got)
		}
	}
	if !UnixTime(0).IsZero() {
		t.Fatal("zero must stay zero")
	}
}

func TestMediaKindForAttachment(t *testing.T) {
	cases := map[string]string{"image": "image", "video": "video", "ig_reel": "video", "reel": "video", "audio": "audio", "file": "document", "sticker": "", "fallback": ""}
	for in, want := range cases {
		if got := MediaKindForAttachment(in); got != want {
			t.Fatalf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestEntryDedupKeyDependsOnTheWholeEntry(t *testing.T) {
	one := &EntryEnvelope{Object: "page", Entry: &Entry{ID: "P", Messaging: []*MessagingEvent{{Timestamp: 1, Message: &Message{MID: "a"}}}}}
	same := &EntryEnvelope{Object: "page", Entry: &Entry{ID: "P", Messaging: []*MessagingEvent{{Timestamp: 1, Message: &Message{MID: "a"}}}}}
	more := &EntryEnvelope{Object: "page", Entry: &Entry{ID: "P", Messaging: []*MessagingEvent{
		{Timestamp: 1, Message: &Message{MID: "a"}},
		{Timestamp: 2, Message: &Message{MID: "b"}},
	}}}
	d := Dialect{Prefix: "fb"}
	if d.EntryDedupKey(one) != d.EntryDedupKey(same) {
		t.Fatal("identical entries must share a key")
	}
	if d.EntryDedupKey(one) == d.EntryDedupKey(more) {
		t.Fatal("an entry with extra events must not be deduplicated against the smaller one")
	}
	if d.EntryDedupKey(nil) != "" {
		t.Fatal("nil entry must have no key")
	}
}
