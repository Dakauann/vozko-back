package marketing

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestListPixelsMapsTimesAndAvailability(t *testing.T) {
	g, log := wireGateway(t, reply(`{"data":[{"id":"300","name":"Site","last_fired_time":"2026-10-01T08:00:00+0000","creation_time":"2025-01-02T03:04:05+0000","is_unavailable":true},{"id":"301","name":"Novo"}]}`))
	pixels, err := g.ListPixels(context.Background(), "tok", "77")
	if err != nil {
		t.Fatal(err)
	}
	if call := log.all()[0]; call.path != "/act_77/adspixels" || call.query.Get("fields") != pixelFields {
		t.Fatalf("call = %+v", call)
	}
	if len(pixels) != 2 || pixels[0].MetaID != "300" || !pixels[0].Unavailable || pixels[0].LastFiredTime == nil ||
		!pixels[0].LastFiredTime.Equal(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)) || pixels[0].CreationTime == nil ||
		pixels[1].LastFiredTime != nil || pixels[1].Unavailable {
		t.Fatalf("pixels = %+v", pixels)
	}
	broken, _ := wireGateway(t, reply(`{"data":[{"id":"300","last_fired_time":"yesterday"}]}`))
	if _, err := broken.ListPixels(context.Background(), "tok", "77"); err == nil {
		t.Fatal("expected an error for an unparseable time")
	}
}

func TestCreatePixelPostsName(t *testing.T) {
	g, log := wireGateway(t, reply(`{"id":"302"}`))
	id, err := g.CreatePixel(context.Background(), "tok", "act_77", "Vozko site")
	if err != nil || id != "302" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	if call := log.all()[0]; call.method != http.MethodPost || call.path != "/act_77/adspixels" || call.form.Get("name") != "Vozko site" {
		t.Fatalf("call = %+v", call)
	}
}

func TestDatasetForWABAReturnsTheExistingDataset(t *testing.T) {
	for _, body := range []string{`{"id":"400"}`, `{"data":[{"id":"400"}]}`} {
		g, log := wireGateway(t, reply(body))
		id, err := g.DatasetForWABA(context.Background(), "tok", "999")
		if err != nil || id != "400" {
			t.Fatalf("%s: id = %q err = %v", body, id, err)
		}
		if calls := log.all(); len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/999/dataset" {
			t.Fatalf("calls = %+v", calls)
		}
	}
}

func TestDatasetForWABACreatesWhenAbsent(t *testing.T) {
	g, log := wireGateway(t, func(call wireCall) (int, string) {
		if call.method == http.MethodGet {
			return http.StatusOK, `{"data":[]}`
		}
		return http.StatusOK, `{"id":"401"}`
	})
	id, err := g.DatasetForWABA(context.Background(), "tok", "999")
	if err != nil || id != "401" {
		t.Fatalf("id = %q err = %v", id, err)
	}
	if calls := log.all(); len(calls) != 2 || calls[1].method != http.MethodPost || calls[1].path != "/999/dataset" {
		t.Fatalf("calls = %+v", calls)
	}
	ambiguous, _ := wireGateway(t, reply(`{"data":[{"id":"1"},{"id":"2"}]}`))
	if _, err := ambiguous.DatasetForWABA(context.Background(), "tok", "999"); err == nil {
		t.Fatal("expected an error for more than one dataset")
	}
}

var eventTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func whatsappPurchase() advertising.ConversionEvent {
	return advertising.ConversionEvent{
		EventID: "opp1:Purchase", Name: advertising.EventNamePurchase, Time: eventTime,
		Target: advertising.TargetDataset, TargetID: "400", ActionSource: "business_messaging",
		Identity:    advertising.MessagingIdentity{Channel: advertising.ChannelWhatsApp, ClickID: "clid", WABAID: "999"},
		ValueMicros: 350_500_000, Currency: "BRL",
	}
}

func TestSendEventsSerializesEachIdentity(t *testing.T) {
	g, log := wireGateway(t, func(call wireCall) (int, string) {
		var body struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(call.body, &body)
		return http.StatusOK, `{"events_received":` + strconv.Itoa(len(body.Data)) + `}`
	})
	messenger := advertising.ConversionEvent{
		EventID: "opp2:LeadSubmitted", Name: advertising.EventNameLead, Time: eventTime,
		Target: advertising.TargetDataset, TargetID: "400", ActionSource: "business_messaging",
		Identity: advertising.MessagingIdentity{Channel: advertising.ChannelMessenger, PageID: "55", PageScopedUserID: "psid"},
	}
	instagram := advertising.ConversionEvent{
		EventID: "opp3:LeadSubmitted", Name: advertising.EventNameLead, Time: eventTime,
		Target: advertising.TargetDataset, TargetID: "410", ActionSource: "business_messaging",
		Identity: advertising.MessagingIdentity{Channel: advertising.ChannelInstagram, InstagramUserID: "ig1", InstagramScoped: "igsid"},
	}
	pixel := advertising.ConversionEvent{
		EventID: "opp4:LeadSubmitted", Name: advertising.EventNameLead, Time: eventTime,
		Target: advertising.TargetPixel, TargetID: "300", ActionSource: "system_generated", EmailHash: "emhash",
	}
	if err := g.SendEvents(context.Background(), "tok", []advertising.ConversionEvent{whatsappPurchase(), pixel, messenger, instagram}); err != nil {
		t.Fatal(err)
	}
	calls := log.all()
	if len(calls) != 3 || calls[0].path != "/400/events" || calls[1].path != "/300/events" || calls[2].path != "/410/events" || calls[0].method != http.MethodPost {
		t.Fatalf("calls = %+v", calls)
	}
	unix := strconv.FormatInt(eventTime.Unix(), 10)
	sameJSON(t, calls[0].body, `{"data":[
		{"event_name":"Purchase","event_time":`+unix+`,"event_id":"opp1:Purchase","action_source":"business_messaging","messaging_channel":"whatsapp",
		 "user_data":{"whatsapp_business_account_id":"999","ctwa_clid":"clid"},"custom_data":{"value":350.5,"currency":"BRL"}},
		{"event_name":"LeadSubmitted","event_time":`+unix+`,"event_id":"opp2:LeadSubmitted","action_source":"business_messaging","messaging_channel":"messenger",
		 "user_data":{"page_id":"55","page_scoped_user_id":"psid"}}]}`)
	sameJSON(t, calls[1].body, `{"data":[{"event_name":"LeadSubmitted","event_time":`+unix+`,"event_id":"opp4:LeadSubmitted","action_source":"system_generated","user_data":{"em":["emhash"]}}]}`)
	sameJSON(t, calls[2].body, `{"data":[{"event_name":"LeadSubmitted","event_time":`+unix+`,"event_id":"opp3:LeadSubmitted","action_source":"business_messaging","messaging_channel":"instagram",
		"user_data":{"ig_account_id":"ig1","ig_sid":"igsid"}}]}`)
}

func TestMajorUnitsKeepsExactDecimals(t *testing.T) {
	for micros, want := range map[int64]string{350_500_000: "350.5", 1_000_000: "1", 1: "0.000001", 199_990_000: "199.99"} {
		if got := majorUnits(micros); string(got) != want {
			t.Fatalf("majorUnits(%d) = %s, want %s", micros, got, want)
		}
	}
}

func TestSendEventsSplitsAtOneThousand(t *testing.T) {
	var sizes []int
	g, _ := wireGateway(t, func(call wireCall) (int, string) {
		var body struct {
			Data []json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(call.body, &body)
		sizes = append(sizes, len(body.Data))
		return http.StatusOK, `{"events_received":` + strconv.Itoa(len(body.Data)) + `}`
	})
	events := make([]advertising.ConversionEvent, 1001)
	for i := range events {
		events[i] = whatsappPurchase()
	}
	if err := g.SendEvents(context.Background(), "tok", events); err != nil {
		t.Fatal(err)
	}
	if len(sizes) != 2 || sizes[0] != 1000 || sizes[1] != 1 {
		t.Fatalf("sizes = %v", sizes)
	}
}

func TestSendEventsFailsWhenMetaReceivedFewer(t *testing.T) {
	for _, body := range []string{`{"events_received":1}`, `{"messages":[]}`} {
		g, _ := wireGateway(t, reply(body))
		if err := g.SendEvents(context.Background(), "tok", []advertising.ConversionEvent{whatsappPurchase(), whatsappPurchase()}); err == nil {
			t.Fatalf("%s: expected an error", body)
		}
	}
}

func TestSendEventsRefusesUnserializableEventsBeforeCalling(t *testing.T) {
	g, log := wireGateway(t, reply(`{"events_received":1}`))
	unknownSource := whatsappPurchase()
	unknownSource.ActionSource = "website"
	unknownChannel := whatsappPurchase()
	unknownChannel.Identity.Channel = "telegram"
	noValue := whatsappPurchase()
	noValue.ValueMicros = 0
	noHash := advertising.ConversionEvent{EventID: "x", Name: advertising.EventNameLead, Time: eventTime, Target: advertising.TargetPixel, TargetID: "300", ActionSource: "system_generated"}
	noTarget := whatsappPurchase()
	noTarget.TargetID = ""
	for name, bad := range map[string]advertising.ConversionEvent{"source": unknownSource, "channel": unknownChannel, "value": noValue, "hash": noHash, "target": noTarget} {
		if err := g.SendEvents(context.Background(), "tok", []advertising.ConversionEvent{whatsappPurchase(), bad}); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if len(log.all()) != 0 {
		t.Fatalf("calls = %+v", log.all())
	}
}

func TestSendEventsWithNothingToSendMakesNoCall(t *testing.T) {
	g, log := wireGateway(t, reply(`{}`))
	if err := g.SendEvents(context.Background(), "tok", nil); err != nil {
		t.Fatal(err)
	}
	if len(log.all()) != 0 {
		t.Fatalf("calls = %+v", log.all())
	}
}
