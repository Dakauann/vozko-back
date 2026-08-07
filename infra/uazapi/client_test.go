package uazapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// The most consequential code in this package is decodeError: the host's own
// failures and WhatsApp's forwarded refusals arrive through the same HTTP
// status, and only the body tells them apart. Getting it wrong means retrying
// into a WhatsApp limit, which is how a temporary warning becomes a permanent
// ban.

func TestDecodeErrorClassifiesAWhatsAppRestriction(t *testing.T) {
	body := `{
		"error": "cannot start new conversation",
		"error_source": "whatsapp_server",
		"provider": "whatsapp",
		"provider_code": 463,
		"error_key": "WHATSAPP_REACHOUT_TIMELOCK",
		"provider_message_ptbr": "O WhatsApp informou uma restrição temporária.",
		"details": {
			"reachout_timelock": {
				"available": true,
				"active": true,
				"until": "2026-08-07T12:00:00Z",
				"enforcement_type": "BIZ_QUALITY"
			}
		}
	}`

	err := decodeError(http.StatusBadRequest, []byte(body))
	provErr, ok := uw.AsProviderError(err)
	if !ok {
		t.Fatalf("expected a ProviderError, got %T", err)
	}

	if !provErr.IsRestriction() {
		t.Error("a provider_code 463 must be recognised as a WhatsApp restriction")
	}
	// The decisive assertion: a restriction must never be retried. Retrying is
	// exactly the behaviour that escalates a limit into a ban.
	if provErr.Retryable() {
		t.Error("a WhatsApp restriction must not be retryable")
	}
	if provErr.LocalizedMessage == "" {
		t.Error("the provider's own pt-BR wording must survive; it is what the operator is shown")
	}

	if provErr.Restriction == nil {
		t.Fatal("the restriction detail must be parsed for the circuit breaker")
	}
	if provErr.Restriction.CanSendNewChats == nil || *provErr.Restriction.CanSendNewChats {
		t.Error("an active timelock means new conversations are blocked")
	}
	if provErr.Restriction.Until == nil {
		t.Error("the restriction window must be captured so the UI can say when it lifts")
	}
}

// A quota that is merely counted, not exhausted, is not a refusal. Treating it
// as one would pause a broadcast that is still allowed to run.
func TestDecodeErrorQuotaNotYetExhausted(t *testing.T) {
	body := `{
		"error": "capped",
		"provider_code": 463,
		"details": {
			"new_chat_message_capping": {
				"available": true, "status": "OK", "used_quota": 3, "total_quota": 10
			}
		}
	}`

	provErr, ok := uw.AsProviderError(decodeError(http.StatusBadRequest, []byte(body)))
	if !ok {
		t.Fatal("expected a ProviderError")
	}
	if provErr.Restriction == nil {
		t.Fatal("quota detail must be parsed")
	}
	if provErr.Restriction.CanSendNewChats == nil || !*provErr.Restriction.CanSendNewChats {
		t.Error("3 of 10 used is not exhausted; sending is still permitted")
	}
	if provErr.Restriction.UsedQuota != 3 || provErr.Restriction.TotalQuota != 10 {
		t.Errorf("quota = %d/%d, want 3/10",
			provErr.Restriction.UsedQuota, provErr.Restriction.TotalQuota)
	}
}

// A plain host failure must NOT masquerade as a WhatsApp restriction, or the
// circuit breaker pauses broadcasts for the wrong reason.
func TestDecodeErrorPlainHostFailure(t *testing.T) {
	provErr, ok := uw.AsProviderError(decodeError(http.StatusInternalServerError, []byte(`{"error":"No session"}`)))
	if !ok {
		t.Fatal("expected a ProviderError")
	}
	if provErr.IsRestriction() {
		t.Error("a host error is not a WhatsApp restriction")
	}
	if !provErr.Retryable() {
		t.Error("a 5xx from the host is retryable")
	}
}

func TestDecodeErrorClassification(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		wantRetry     bool
		wantReconnect bool
		wantCapacity  bool
	}{
		{"unauthorised means the session is gone", http.StatusUnauthorized, false, true, false},
		{"host at its instance ceiling", http.StatusTooManyRequests, true, false, true},
		{"transient capacity refusal", http.StatusServiceUnavailable, true, false, true},
		{"bad gateway", http.StatusBadGateway, true, false, false},
		{"plain bad request", http.StatusBadRequest, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provErr, ok := uw.AsProviderError(decodeError(tc.status, []byte(`{"error":"x"}`)))
			if !ok {
				t.Fatal("expected a ProviderError")
			}
			if provErr.Retryable() != tc.wantRetry {
				t.Errorf("Retryable() = %v, want %v", provErr.Retryable(), tc.wantRetry)
			}
			if provErr.NeedsReconnect() != tc.wantReconnect {
				t.Errorf("NeedsReconnect() = %v, want %v", provErr.NeedsReconnect(), tc.wantReconnect)
			}
			if provErr.AtCapacity() != tc.wantCapacity {
				t.Errorf("AtCapacity() = %v, want %v", provErr.AtCapacity(), tc.wantCapacity)
			}
		})
	}
}

// A body that does not parse must still yield a classifiable error. Returning a
// bare "unexpected response" would make a 401 indistinguishable from a 503 and
// break both the reconnect and the capacity paths.
func TestDecodeErrorSurvivesAnUnparseableBody(t *testing.T) {
	provErr, ok := uw.AsProviderError(decodeError(http.StatusUnauthorized, []byte("<html>gateway timeout</html>")))
	if !ok {
		t.Fatal("expected a ProviderError even for a non-JSON body")
	}
	if !provErr.NeedsReconnect() {
		t.Error("the status must still classify the failure")
	}
	if provErr.Message == "" {
		t.Error("some diagnostic text must survive")
	}
}

// ---------------------------------------------------------------- transport

func TestCreateInstanceSendsAdminTokenAndTracingMetadata(t *testing.T) {
	var gotAdminToken, gotPath string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAdminToken = r.Header.Get(headerAdminToken)
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{"id": "r18", "name": "vozko-abc"},
			"token":    "instance-token",
		})
	}))
	defer server.Close()

	created, err := NewClient(Config{}).CreateInstance(context.Background(),
		uw.ServerRef{BaseURL: server.URL, AdminToken: "admin-secret"},
		uw.CreateInstanceInput{Name: "vozko-abc", WorkspaceID: "ws-1", OurInstanceID: "inst-1"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	if gotAdminToken != "admin-secret" {
		t.Errorf("admin token header = %q", gotAdminToken)
	}
	if gotPath != "/instance/create" {
		t.Errorf("path = %q", gotPath)
	}
	// The admin metadata is what makes an instance stranded on a host traceable
	// back to a tenant after a crash between the remote create and our write.
	if gotBody["adminField01"] != "ws-1" || gotBody["adminField02"] != "inst-1" {
		t.Errorf("tracing metadata not sent: %v", gotBody)
	}
	if created.ProviderInstanceID != "r18" || created.Token != "instance-token" {
		t.Errorf("created = %+v", created)
	}
}

// An instance created without an addressable id or token is unusable and can
// never be cleaned up by id. Failing loudly beats persisting a ghost.
func TestCreateInstanceRejectsAnUnaddressableResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"instance": map[string]any{"id": "r18"}})
	}))
	defer server.Close()

	_, err := NewClient(Config{}).CreateInstance(context.Background(),
		uw.ServerRef{BaseURL: server.URL, AdminToken: "admin"},
		uw.CreateInstanceInput{Name: "x"})
	if err == nil {
		t.Fatal("an instance with no token must be rejected")
	}
}

// Connect switches mode by the PRESENCE of a phone number on the wire, which is
// why the domain carries an explicit mode: an empty phone in pairing mode would
// silently fall back to a QR the customer is not looking at.
func TestConnectModeSelection(t *testing.T) {
	t.Run("qr omits the phone", func(t *testing.T) {
		var body map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instance": map[string]any{"status": "connecting", "qrcode": "data:image/png;base64,AAA"},
			})
		}))
		defer server.Close()

		session, err := NewClient(Config{}).Connect(context.Background(),
			uw.InstanceRef{BaseURL: server.URL, Token: "t"}, uw.ConnectInput{Mode: uw.ConnectModeQR})
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if _, present := body["phone"]; present {
			t.Error("QR mode must not send a phone number, or the host returns a pairing code")
		}
		if session.QRCode == "" || session.State != "connecting" {
			t.Errorf("session = %+v", session)
		}
	})

	t.Run("pairing normalises the phone", func(t *testing.T) {
		var body map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instance": map[string]any{"status": "connecting", "paircode": "1234-5678"},
			})
		}))
		defer server.Close()

		session, err := NewClient(Config{}).Connect(context.Background(),
			uw.InstanceRef{BaseURL: server.URL, Token: "t"},
			uw.ConnectInput{Mode: uw.ConnectModePairing, Phone: "+55 (11) 99999-9999"})
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		if body["phone"] != "5511999999999" {
			t.Errorf("phone sent as %v, want digits only", body["phone"])
		}
		if session.PairCode != "1234-5678" {
			t.Errorf("pair code = %q", session.PairCode)
		}
	})

	t.Run("pairing without a phone is refused before the call", func(t *testing.T) {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))
		defer server.Close()

		_, err := NewClient(Config{}).Connect(context.Background(),
			uw.InstanceRef{BaseURL: server.URL, Token: "t"}, uw.ConnectInput{Mode: uw.ConnectModePairing})
		if err == nil {
			t.Fatal("pairing without a phone must fail")
		}
		if called {
			t.Error("the host must not be called for a request that cannot succeed")
		}
	})
}

// /instance/connect answers with the flags at the top level and /instance/status
// nests them under "status". Both must normalise to the same Session, or a
// connected number reads as disconnected on one of the two paths.
func TestStatusNormalisesTheNestedResponseShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"instance": map[string]any{
				"status":      "connected",
				"profileName": "Loja ABC",
				"isBusiness":  true,
				"plataform":   "Android",
			},
			"status": map[string]any{
				"connected": true,
				"loggedIn":  true,
				"jid":       "5511999999999@s.whatsapp.net",
			},
		})
	}))
	defer server.Close()

	session, err := NewClient(Config{}).Status(context.Background(),
		uw.InstanceRef{BaseURL: server.URL, Token: "t"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !session.Connected || !session.LoggedIn {
		t.Error("the nested flags were not read")
	}
	if session.JID != "5511999999999@s.whatsapp.net" {
		t.Errorf("jid = %q", session.JID)
	}
	// The vendor spells this field "plataform"; a silent typo here loses the
	// platform on every instance.
	if session.Platform != "Android" {
		t.Errorf("platform = %q", session.Platform)
	}
	if !session.IsBusiness || session.ProfileName != "Loja ABC" {
		t.Errorf("session = %+v", session)
	}
}

// The jid field is null when logged out, a string when connected, and sometimes
// an object. A decoder that assumed one shape would fail the whole poll.
func TestJIDDecodingToleratesEveryShape(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"null when logged out", nil, ""},
		{"string when connected", "5511999999999@s.whatsapp.net", "5511999999999@s.whatsapp.net"},
		{"object with parts", map[string]any{"User": "5511999999999", "Server": "s.whatsapp.net"}, "5511999999999@s.whatsapp.net"},
		{"object defaulting the server", map[string]any{"user": "5511999999999"}, "5511999999999@s.whatsapp.net"},
		{"unexpected type", 42, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jidString(tc.value); got != tc.want {
				t.Errorf("jidString(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// Our registration must never carry an exclusion filter. Excluding API-sent
// messages — which the vendor's docs recommend — would silently cost the
// delivery-status track and every message an operator types on their own phone.
func TestSetWebhookRegistersWithoutExclusions(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := NewClient(Config{}).SetWebhook(context.Background(),
		uw.InstanceRef{BaseURL: server.URL, Token: "t"},
		uw.WebhookSubscription{
			URL:     "https://api.example.com/webhooks/unofficial-whatsapp/tok",
			Enabled: true, Events: uw.SubscribedEvents(), ExcludeMessages: []string{},
		})
	if err != nil {
		t.Fatalf("SetWebhook: %v", err)
	}

	excludes, ok := body["excludeMessages"].([]any)
	if !ok {
		t.Fatalf("excludeMessages must be sent as an explicit array, got %T", body["excludeMessages"])
	}
	if len(excludes) != 0 {
		t.Errorf("no exclusion filter may be registered, got %v", excludes)
	}
	// The event kind is read from the body, which has to be parsed anyway. A
	// second source of truth in the URL is one that can drift.
	if body["addUrlEvents"] != false || body["addUrlTypesMessages"] != false {
		t.Error("URL-decorated events must stay off")
	}
}

func TestMessagingLimitsTreatsAnActiveTimelockAsBlocking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Deliberately absent top-level flag: an active timelock must block
			// on its own, because a missing answer must never read as permission.
			"reachout_timelock": map[string]any{
				"available": true, "active": true, "until": "2026-08-07T12:00:00Z",
			},
		})
	}))
	defer server.Close()

	restriction, err := NewClient(Config{}).MessagingLimits(context.Background(),
		uw.InstanceRef{BaseURL: server.URL, Token: "t"})
	if err != nil {
		t.Fatalf("MessagingLimits: %v", err)
	}
	if restriction.CanSendNewChats == nil || *restriction.CanSendNewChats {
		t.Error("an active timelock must block new conversations")
	}
	if !restriction.Active(time.Now().UTC()) {
		t.Error("the restriction must read as active")
	}
}

func TestParseTimeRejectsGarbage(t *testing.T) {
	if parseTime("") != nil || parseTime("not a date") != nil {
		// A zero time downstream renders as "January 1st, year 1" in the UI,
		// which is worse than an absent value.
		t.Error("an unparseable timestamp must yield nil, never the zero time")
	}
	if got := parseTime("2026-08-07T12:00:00Z"); got == nil || got.Year() != 2026 {
		t.Errorf("RFC3339 must parse, got %v", got)
	}
	if got := parseTime("2026-08-07T12:00:00.123Z"); got == nil {
		t.Error("fractional seconds must parse")
	}
}
