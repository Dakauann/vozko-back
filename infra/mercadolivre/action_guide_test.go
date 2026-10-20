package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestActionGuideOptions(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "action_guide_options_doc_example_fixed.json")))
	limiters := newFakeLimiters()
	s, err := NewActionGuideService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	opts, err := s.Options(context.Background(), sellerCreds, "20000000000")
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodGet || r.Path != "/messages/action_guide/packs/20000000000" || r.Query.Get("tag") != "post_sale" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:messages:read" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if len(opts) != 3 {
		t.Fatalf("options = %+v", opts)
	}
	if opts[0].ID != "REQUEST_VARIANTS" || opts[0].Type != "template" || !opts[0].Enabled || !opts[0].Actionable ||
		opts[0].CapAvailable != 1 || len(opts[0].Templates) != 1 || opts[0].Templates[0].ID != "TEMPLATE___REQUEST_VARIANTS___1" {
		t.Fatalf("first option = %+v", opts[0])
	}
	if len(opts[1].Templates[0].Vars) != 2 || opts[1].Templates[0].Vars[0].Type != "NUMBER" {
		t.Fatalf("delivery promise = %+v", opts[1])
	}
	if opts[0].Description != "This is an example..." || opts[0].Templates[0].TextsBySite != nil {
		t.Fatalf("request variants = %+v", opts[0])
	}
	texts := opts[1].Templates[0].TextsBySite
	if len(texts) != 1 || texts["MLB"] != "Olá,\nEntregaremos sua compra %s entre %d e %d h." {
		t.Fatalf("delivery promise texts = %#v", texts)
	}
	if opts[2].ID != "OTHER" || opts[2].CharLimit != 350 || opts[2].CapAvailable != 0 {
		t.Fatalf("other = %+v", opts[2])
	}
}

func TestActionGuideCaps(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "action_guide_caps_doc_example.json")))
	s, _ := NewActionGuideService(newTestClient(t, fs, newFakeLimiters()))
	caps, err := s.Caps(context.Background(), sellerCreds, "200000000000")
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/messages/action_guide/packs/200000000000/caps_available" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	if len(caps) != 5 || caps[4].OptionID != "OTHER" || caps[4].CapAvailable != 1 {
		t.Fatalf("caps = %+v", caps)
	}
}

func TestActionGuideSendOption(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "option_sent_doc_example.json")))
	limiters := newFakeLimiters()
	s, _ := NewActionGuideService(newTestClient(t, fs, limiters))
	msg, err := s.SendOption(context.Background(), sellerCreds, "2000000000000000", mldomain.ReasonSend{OptionID: mldomain.ReasonOther, Text: "Olá Maria, estou precisando de..."})
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/messages/action_guide/packs/2000000000000000/option" || r.Query.Get("tag") != "post_sale" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:messages:write" {
		t.Fatalf("limiter keys = %v", keys)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body["option_id"] != "OTHER" || body["text"] != "Olá Maria, estou precisando de..." || len(body) != 2 {
		t.Fatalf("body = %s", r.Body)
	}
	if msg.ID != "94353d192b9640e8b1ed3c77aa406f39" || msg.ToUserID != "618491100" || msg.Status != mldomain.MessageAvailable {
		t.Fatalf("message = %+v", msg)
	}
}

func TestActionGuideSendTemplateWithVars(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "option_sent_doc_example.json")))
	s, _ := NewActionGuideService(newTestClient(t, fs, newFakeLimiters()))
	_, err := s.SendOption(context.Background(), sellerCreds, "2000000000000000", mldomain.ReasonSend{
		OptionID:   mldomain.ReasonDeliveryPromise,
		TemplateID: "TEMPLATE___DELIVERY_PROMISE___1",
		Vars: []mldomain.ReasonVarValue{
			{ID: "TEMPLATE___DELIVERY_PROMISE___1___VAR___INIT", Value: 12},
			{ID: "TEMPLATE___DELIVERY_PROMISE___1___VAR___LIMIT", Value: 23},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"option_id":"DELIVERY_PROMISE","template_id":"TEMPLATE___DELIVERY_PROMISE___1","vars":[{"id":"TEMPLATE___DELIVERY_PROMISE___1___VAR___INIT","value":12},{"id":"TEMPLATE___DELIVERY_PROMISE___1___VAR___LIMIT","value":23}]}`
	if strings.TrimSpace(fs.last().Body) != want {
		t.Fatalf("body = %s", fs.last().Body)
	}
}

func TestActionGuideRefusals(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s, _ := NewActionGuideService(newTestClient(t, fs, newFakeLimiters()))
	cases := []struct {
		pack string
		send mldomain.ReasonSend
		err  error
	}{
		{"x", mldomain.ReasonSend{OptionID: "OTHER", Text: "oi"}, mldomain.ErrPackUnknown},
		{"1", mldomain.ReasonSend{Text: "oi"}, errReasonOption},
		{"1", mldomain.ReasonSend{OptionID: "OTHER"}, errReasonContent},
		{"1", mldomain.ReasonSend{OptionID: "OTHER", Text: "Total 10€"}, mldomain.ErrTextCharset},
		{"1", mldomain.ReasonSend{OptionID: "OTHER", Text: strings.Repeat("a", 351)}, mldomain.ErrTextTooLong},
	}
	for i, tc := range cases {
		if _, err := s.SendOption(context.Background(), sellerCreds, tc.pack, tc.send); !errors.Is(err, tc.err) {
			t.Errorf("case %d err = %v, want %v", i, err, tc.err)
		}
	}
	if _, err := s.Options(context.Background(), sellerCreds, "x"); err == nil {
		t.Error("malformed pack accepted")
	}
	if fs.count() != 0 {
		t.Fatal("refusals must not reach Mercado Livre")
	}
}

func TestActionGuideConflictIsNotRetried(t *testing.T) {
	fs := newFakeServer(t, jsonReply(409, `{"status_code":409,"error":"conflict","message":"another request is locking this operation"}`))
	s, _ := NewActionGuideService(newTestClient(t, fs, newFakeLimiters()))
	_, err := s.SendOption(context.Background(), sellerCreds, "1", mldomain.ReasonSend{OptionID: "OTHER", Text: "oi"})
	var mlErr *Error
	if !errors.As(err, &mlErr) || mlErr.HTTPStatus != 409 || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}
