package cmcom

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return body
}

func amsterdam(t *testing.T, layout string) time.Time {
	t.Helper()
	at, err := time.ParseInLocation(ReportTimeLayout, layout, ReportLocation())
	if err != nil {
		t.Fatalf("parse %s: %v", layout, err)
	}
	return at
}

func TestDecodeStatusReportsFixtures(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		file        string
		want        carriermessaging.StatusReport
	}{
		{"delivered", "application/json", "status_delivered.json", carriermessaging.StatusReport{
			Reference: "reference2", Status: carriermessaging.StatusDelivered, ErrorDetail: "Delivered",
			ReceivedAt: amsterdam(t, "2026-05-04T15:38:56"),
		}},
		{"rejected after 24 hours", "application/json; charset=utf-8", "status_rejected.json", carriermessaging.StatusReport{
			Reference: "reference2", Status: carriermessaging.StatusRejected, ErrorCode: "87",
			ErrorDetail: "Message marked as failed by CM after 24 hours without receiving final status from the operator",
			ReceivedAt:  amsterdam(t, "2026-06-17T13:57:13"),
		}},
		{"device without rcs", "text/json", "status_unreachable.json", carriermessaging.StatusReport{
			Reference: "9c5ac3abb8c94775b8d7ceb274ba335a", Status: carriermessaging.StatusFailed, ErrorCode: "42",
			ErrorDetail: "Message not delivered because recipient is not capable to receive a message from this channel",
			Operator:    "20416", Unreachable: true,
			ReceivedAt: amsterdam(t, "2026-01-15T09:12:00"),
		}},
		{"xml read", "application/xml", "status_report.xml", carriermessaging.StatusReport{
			Reference: "reference2", Status: carriermessaging.StatusRead, ErrorDetail: "Read",
			ReceivedAt: amsterdam(t, "2026-05-04T15:38:56"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeStatusReports(tc.contentType, fixture(t, tc.file))
			if err != nil {
				t.Fatalf("DecodeStatusReports: %v", err)
			}
			if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestSMSStatusReportsDecodeTheSameWay(t *testing.T) {
	got, err := DecodeStatusReports("application/json", fixture(t, "status_sms_delivered.json"))
	if err != nil {
		t.Fatalf("DecodeStatusReports: %v", err)
	}
	want := carriermessaging.StatusReport{
		Reference: "b1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6", Status: carriermessaging.StatusDelivered,
		ErrorDetail: "Delivered", Operator: "72405", ReceivedAt: amsterdam(t, "2026-10-21T14:05:09"),
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestStatusReportTimesAreDutchLocalTime(t *testing.T) {
	winter, err := DecodeStatusReports("application/json", fixture(t, "status_unreachable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 15, 8, 12, 0, 0, time.UTC); !winter[0].ReceivedAt.Equal(want) {
		t.Fatalf("winter received = %s, want %s", winter[0].ReceivedAt.UTC(), want)
	}
	summer, err := DecodeStatusReports("application/json", fixture(t, "status_delivered.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 5, 4, 13, 38, 56, 0, time.UTC); !summer[0].ReceivedAt.Equal(want) {
		t.Fatalf("summer received = %s, want %s", summer[0].ReceivedAt.UTC(), want)
	}
}

func TestDecodeStatusReportsBatch(t *testing.T) {
	body := []byte(`{"messages":{"msg":[
		{"received":"2026-03-01T10:00:00","reference":"a1","to":"0031612345678","status":{"code":"0","errorCode":"","errorDescription":"Accepted"},"operator":""},
		{"received":"2026-03-01T10:00:01","reference":"a2","to":"0031612345678","status":{"code":"4","errorCode":"","errorDescription":"Read"},"operator":""}
	]}}`)
	got, err := DecodeStatusReports("application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status != carriermessaging.StatusAccepted || got[1].Status != carriermessaging.StatusRead {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeStatusReportsRefusesBadPayloads(t *testing.T) {
	report := func(code, errorCode, received, reference string) []byte {
		return []byte(`{"messages":{"msg":{"received":"` + received + `","reference":"` + reference + `","to":"0031612345678","status":{"code":"` + code + `","errorCode":"` + errorCode + `","errorDescription":""},"operator":""}}}`)
	}
	cases := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"unknown status code", "application/json", report("5", "", "2026-03-01T10:00:00", "r1")},
		{"numeric looking garbage code", "application/json", report("02", "", "2026-03-01T10:00:00", "r1")},
		{"empty status code", "application/json", report("", "", "2026-03-01T10:00:00", "r1")},
		{"non numeric error code", "application/json", report("3", "Delivered", "2026-03-01T10:00:00", "r1")},
		{"missing received", "application/json", report("2", "", "", "r1")},
		{"received with zone", "application/json", report("2", "", "2026-03-01T10:00:00Z", "r1")},
		{"missing reference", "application/json", report("2", "", "2026-03-01T10:00:00", "")},
		{"no msg", "application/json", []byte(`{"messages":{}}`)},
		{"empty msg list", "application/json", []byte(`{"messages":{"msg":[]}}`)},
		{"msg is a string", "application/json", []byte(`{"messages":{"msg":"x"}}`)},
		{"numeric code value", "application/json", []byte(`{"messages":{"msg":{"received":"2026-03-01T10:00:00","reference":"r","status":{"code":2}}}}`)},
		{"broken json", "application/json", []byte(`{"messages":`)},
		{"form encoded", "application/x-www-form-urlencoded", report("2", "", "2026-03-01T10:00:00", "r1")},
		{"no content type", "", report("2", "", "2026-03-01T10:00:00", "r1")},
		{"broken xml", "text/xml", []byte(`<MESSAGES><MSG>`)},
		{"empty xml", "text/xml", []byte(`<MESSAGES></MESSAGES>`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeStatusReports(tc.contentType, tc.body)
			if !errors.Is(err, ErrPayloadInvalid) {
				t.Fatalf("DecodeStatusReports = %+v, %v; want ErrPayloadInvalid", got, err)
			}
		})
	}
}

func TestDecodeInboundFixtures(t *testing.T) {
	cases := []struct {
		file string
		want carriermessaging.Inbound
	}{
		{"inbound_text.json", carriermessaging.Inbound{
			ProviderMessageID: "2f2d42ac-3809-40fb-bce5-dc720e400000",
			Phone:             "316012345678",
			ContactName:       "Jan",
			To:                "cm.com_agent",
			Channel:           carriermessaging.ChannelRCS,
			Text:              "This is an example message",
			InReplyTo:         "9c5ac3abb8c94775b8d7ceb274ba335a",
			ReceivedAt:        time.Date(2019, 11, 5, 8, 32, 33, 0, time.UTC),
		}},
		{"inbound_media.json", carriermessaging.Inbound{
			ProviderMessageID: "2f2d42ac-3809-40fb-bce5-dc720e400001",
			Phone:             "31612345678",
			To:                "cm.com_agent",
			Channel:           carriermessaging.ChannelRCS,
			Media:             &carriermessaging.InboundMedia{URL: "https://temp.com/adsadsa", MIMEType: "image/png", Name: "Media items can be empty"},
			ReceivedAt:        time.Date(2019, 11, 5, 8, 32, 33, 0, time.UTC),
		}},
		{"inbound_sms.json", carriermessaging.Inbound{
			ProviderMessageID: "2f2d42ac-3809-40fb-bce5-dc720e400002",
			Phone:             "316012345678",
			To:                "3669",
			Channel:           carriermessaging.ChannelSMS,
			Text:              "This is an example message",
			ReceivedAt:        time.Date(2019, 11, 5, 8, 32, 33, 0, time.UTC),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, err := DecodeInbound(fixture(t, tc.file))
			if err != nil {
				t.Fatalf("DecodeInbound: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDecodeInboundCarriesAReplyPostback(t *testing.T) {
	body := []byte(`{"from":{"number":"0031612345678","name":""},"to":{"number":"00agent"},"message":{"text":"Sim","custom":{"postbackdata":"vz:m0"}},"reference":"ref-1","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`)
	got, err := DecodeInbound(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.Postback != "vz:m0" || got.Text != "Sim" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodeInboundSMSReceivingNumber(t *testing.T) {
	cases := []struct {
		to   string
		want string
	}{
		{"3669", "3669"},
		{"29292", "29292"},
		{"+5511912345678", "5511912345678"},
		{"005511912345678", "5511912345678"},
		{"5511912345678", "5511912345678"},
	}
	for _, tc := range cases {
		t.Run(tc.to, func(t *testing.T) {
			body := []byte(`{"from":{"number":"+5511987654321"},"to":{"number":"` + tc.to + `"},"message":{"text":"SAIR"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`)
			got, err := DecodeInbound(body)
			if err != nil {
				t.Fatalf("DecodeInbound: %v", err)
			}
			if got.To != tc.want || got.Channel != carriermessaging.ChannelSMS || got.Phone != "5511987654321" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDecodeInboundRefusesBadPayloads(t *testing.T) {
	cases := map[string]string{
		"sms to an agent id":  `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`,
		"sms to too short":    `{"from":{"number":"0031612345678"},"to":{"number":"12"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`,
		"sms to with spaces":  `{"from":{"number":"0031612345678"},"to":{"number":"36 69"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`,
		"sms to empty":        `{"from":{"number":"0031612345678"},"to":{"number":""},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`,
		"viber channel":       `{"from":{"number":"0031612345678"},"to":{"number":"3669"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"Viber"}`,
		"other channel":       `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"WhatsApp"}`,
		"missing channel":     `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00"}`,
		"bad phone":           `{"from":{"number":"abc"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing agent":       `{"from":{"number":"0031612345678"},"to":{"number":""},"message":{"text":"x"},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing reference":   `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"bad time":            `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","timeUtc":"yesterday","channel":"RCS"}`,
		"missing time":        `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x"},"reference":"r","channel":"RCS"}`,
		"nothing inside":      `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"  ","media":{"mediaUri":"","contentType":"","title":""}},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"media without type":  `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"media":{"mediaUri":"https://x.example/a","contentType":""}},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"postback not string": `{"from":{"number":"0031612345678"},"to":{"number":"00agent"},"message":{"text":"x","custom":{"postbackdata":7}},"reference":"r","timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"broken json":         `{"from":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeInbound([]byte(body))
			if !errors.Is(err, ErrPayloadInvalid) {
				t.Fatalf("DecodeInbound = %+v, %v; want ErrPayloadInvalid", got, err)
			}
		})
	}
}

func TestDecodeEventFixtures(t *testing.T) {
	cases := []struct {
		file string
		want carriermessaging.SuggestionClick
	}{
		{"event_action.json", carriermessaging.SuggestionClick{
			EventID: "a73bc8c0-ecd6-4b7b-a7de-b6b8e527bf73", Reference: "your_reference",
			Phone: "3161234678", To: "cm.com_agent", Postback: "Call us", Label: "Call us",
			OccurredAt: time.Date(2025, 10, 23, 12, 14, 17, 0, time.UTC),
		}},
		{"event_reply.json", carriermessaging.SuggestionClick{
			EventID: "a73bc8c0-ecd6-4b7b-a7de-b6b8e527bf74", Reference: "your_reference",
			Phone: "3161234678", To: "cm.com_agent", Postback: "Call us",
			OccurredAt: time.Date(2025, 10, 23, 12, 14, 17, 0, time.UTC),
		}},
		{"event_generic.json", carriermessaging.SuggestionClick{
			EventID: "121c38a5-cf77-424a-9ea9-d6c044a845f8", Reference: "msg$abeade67-de9c-45ba-bcfd-abcae1d1879f",
			Phone: "31612345678", To: "3222556611", Postback: "btn$123456", Label: "View on our website",
			OccurredAt: time.Date(2025, 9, 3, 8, 0, 5, 0, time.UTC),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got, ok, err := DecodeEvent(fixture(t, tc.file))
			if err != nil || !ok {
				t.Fatalf("DecodeEvent = %v, %v", ok, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestDecodeEventSkipsOtherEventTypes(t *testing.T) {
	got, ok, err := DecodeEvent(fixture(t, "event_other.json"))
	if err != nil || ok || got != (carriermessaging.SuggestionClick{}) {
		t.Fatalf("DecodeEvent = %+v, %v, %v", got, ok, err)
	}
}

func TestDecodeEventRefusesBadPayloads(t *testing.T) {
	cases := map[string]string{
		"missing type":      `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing event":     `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"other channel":     `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"Instagram"}`,
		"sms channel":       `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"3669"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"SMS"}`,
		"missing context":   `{"reference":"e1","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing postback":  `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"label":"x"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing event id":  `{"messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"bad phone":         `{"reference":"e1","messageContext":"r","from":{"number":"x"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"missing agent":     `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"bad time":          `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":"p"}},"timeUtc":"03/01/2026","channel":"RCS"}`,
		"postback not text": `{"reference":"e1","messageContext":"r","from":{"number":"0031612345678"},"to":{"number":"00agent"},"event":{"type":"SuggestionClick","custom":{"postbackdata":["p"]}},"timeUtc":"2026-03-01T10:00:00","channel":"RCS"}`,
		"broken json":       `{"event":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok, err := DecodeEvent([]byte(body))
			if !errors.Is(err, ErrPayloadInvalid) || ok {
				t.Fatalf("DecodeEvent = %+v, %v, %v; want ErrPayloadInvalid", got, ok, err)
			}
		})
	}
}

func TestStatusCodeRoundTrip(t *testing.T) {
	for _, s := range []carriermessaging.Status{carriermessaging.StatusAccepted, carriermessaging.StatusRejected, carriermessaging.StatusDelivered, carriermessaging.StatusFailed, carriermessaging.StatusRead} {
		code, err := StatusCode(s)
		if err != nil {
			t.Fatalf("StatusCode(%s): %v", s, err)
		}
		if back, ok := statusForCode[code]; !ok || back != s {
			t.Fatalf("code %q maps back to %q, want %q", code, back, s)
		}
	}
	for _, s := range []carriermessaging.Status{carriermessaging.StatusPending, carriermessaging.StatusExpired, carriermessaging.Status("bogus")} {
		if _, err := StatusCode(s); err == nil {
			t.Fatalf("StatusCode(%s) must fail", s)
		}
	}
}
