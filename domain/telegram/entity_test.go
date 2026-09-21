package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestStatusTransitions(t *testing.T) {
	allowed := map[Status][]Status{
		StatusPending:        {StatusActive, StatusRevoked, StatusTokenInvalid},
		StatusActive:         {StatusTokenInvalid, StatusWebhookFailing, StatusRevoked},
		StatusWebhookFailing: {StatusActive, StatusTokenInvalid, StatusRevoked},
		StatusTokenInvalid:   {StatusActive, StatusRevoked},
		StatusRevoked:        {StatusActive},
	}
	all := []Status{
		StatusPending, StatusActive, StatusTokenInvalid, StatusWebhookFailing, StatusRevoked,
	}

	for from, wants := range allowed {
		permitted := map[Status]bool{from: true}
		for _, w := range wants {
			permitted[w] = true
		}
		for _, to := range all {
			got := from.CanTransitionTo(to)
			if got != permitted[to] {
				t.Errorf("%s -> %s = %t, want %t", from, to, got, permitted[to])
			}
		}
	}

	if StatusActive.CanTransitionTo("NONSENSE") {
		t.Error("an unknown status must never be a valid target")
	}
}

func TestAccountCanSend(t *testing.T) {
	base := func() *Account {
		return &Account{
			Mode:      ModeBot,
			BotUserID: 77777,
			BotToken:  "123456:secret",
			Status:    StatusActive,
		}
	}

	if !base().CanSend() {
		t.Error("an active bot with a token must be sendable")
	}

	noToken := base()
	noToken.BotToken = ""
	if noToken.CanSend() {
		t.Error("an account with no token cannot send")
	}

	failing := base()
	failing.Status = StatusWebhookFailing
	if !failing.CanSend() {
		t.Error("a webhook-failing account must still be able to send")
	}

	dead := base()
	dead.Status = StatusTokenInvalid
	if dead.CanSend() {
		t.Error("a revoked token cannot send")
	}

	business := base()
	business.Mode = ModeBusiness
	business.BusinessEnabled = true
	business.BusinessRights = &BusinessRights{CanReply: true}
	if !business.CanSend() {
		t.Error("an enabled business connection with can_reply must be sendable")
	}

	business.BusinessRights = &BusinessRights{CanReply: false}
	if business.CanSend() {
		t.Error("without can_reply a business connection cannot send")
	}

	business.BusinessRights = &BusinessRights{CanReply: true}
	business.BusinessEnabled = false
	if business.CanSend() {
		t.Error("a disabled business connection cannot send")
	}
}

func TestRightsIsNilSafe(t *testing.T) {
	var a Account
	if a.Rights().CanReply {
		t.Error("the zero value must grant nothing")
	}
}

func TestWebhookUnhealthy(t *testing.T) {
	clean := &Account{WebhookPendingCount: 0}
	if clean.WebhookUnhealthy(20) {
		t.Error("a clean webhook is healthy")
	}

	backlog := &Account{WebhookPendingCount: 25}
	if !backlog.WebhookUnhealthy(20) {
		t.Error("a backlog at or above the threshold is unhealthy")
	}

	erroring := &Account{WebhookLastError: "connection refused"}
	if !erroring.WebhookUnhealthy(20) {
		t.Error("a reported delivery error is unhealthy even with no backlog")
	}
}

func TestAccountValidate(t *testing.T) {
	valid := &Account{WorkspaceID: "ws-1", BotUserID: 1, Mode: ModeBot, Status: StatusActive}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}

	cases := map[string]*Account{
		"no workspace": {BotUserID: 1, Mode: ModeBot, Status: StatusActive},
		"no bot id":    {WorkspaceID: "ws-1", Mode: ModeBot, Status: StatusActive},
		"bad mode":     {WorkspaceID: "ws-1", BotUserID: 1, Mode: "NOPE", Status: StatusActive},
		"bad status":   {WorkspaceID: "ws-1", BotUserID: 1, Mode: ModeBot, Status: "NOPE"},
	}
	for name, a := range cases {
		if err := a.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestNormalizeStripsAtPrefix(t *testing.T) {
	a := &Account{BotUsername: " @vozko_bot ", BusinessUsername: "@loja"}
	a.Normalize()

	if a.BotUsername != "vozko_bot" {
		t.Errorf("BotUsername = %q, want the bare handle", a.BotUsername)
	}
	if a.BusinessUsername != "loja" {
		t.Errorf("BusinessUsername = %q, want the bare handle", a.BusinessUsername)
	}
	if a.Mode != ModeBot || a.Status != StatusPending {
		t.Error("Normalize must supply the safe defaults")
	}
}

func TestContactDisplayNameAndHandle(t *testing.T) {
	full := &Contact{FirstName: "Marina", LastName: "Alves", Username: "marina", TGUserID: 42}
	if full.DisplayName() != "Marina Alves" {
		t.Errorf("DisplayName = %q", full.DisplayName())
	}
	if full.Handle() != "@marina" {
		t.Errorf("Handle = %q", full.Handle())
	}

	handleOnly := &Contact{Username: "marina", TGUserID: 42}
	if handleOnly.DisplayName() != "@marina" {
		t.Errorf("DisplayName = %q, want the handle", handleOnly.DisplayName())
	}

	bare := &Contact{TGUserID: 42}
	if bare.DisplayName() != "42" {
		t.Errorf("DisplayName = %q, want the id as a last resort", bare.DisplayName())
	}
	if bare.Handle() != "" {
		t.Errorf("Handle = %q, want empty", bare.Handle())
	}

	phone := "5511987654321"
	shared := &Contact{TGUserID: 42, PhoneNumber: &phone}
	if shared.Handle() != phone {
		t.Errorf("Handle = %q, want the consented phone", shared.Handle())
	}
}

func TestBusinessWindowOpen(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	fresh := now.Add(-time.Hour)
	conv := &Conversation{LastCustomerMessageAt: &fresh}
	open, expires := conv.BusinessWindowOpen(now)
	if !open {
		t.Error("an hour-old inbound leaves the window open")
	}
	if expires == nil || !expires.Equal(fresh.Add(24*time.Hour)) {
		t.Errorf("expiry = %v, want inbound + 24h", expires)
	}

	stale := now.Add(-25 * time.Hour)
	conv.LastCustomerMessageAt = &stale
	if open, _ := conv.BusinessWindowOpen(now); open {
		t.Error("a 25-hour-old inbound closes the window")
	}

	empty := &Conversation{}
	if open, expires := empty.BusinessWindowOpen(now); open || expires != nil {
		t.Error("with no inbound the window is closed and has no expiry")
	}
}

func TestConversationIsPrivate(t *testing.T) {
	if !(&Conversation{}).IsPrivate() {
		t.Error("an unset chat type defaults to private")
	}
	if !(&Conversation{ChatType: ChatTypePrivate}).IsPrivate() {
		t.Error("private is private")
	}
	if (&Conversation{ChatType: ChatTypeSupergroup}).IsPrivate() {
		t.Error("a supergroup is not private, automation must not run there")
	}
}

func TestDeepLinkURLAndExpiry(t *testing.T) {
	link := &DeepLink{Token: "abc123"}
	if got := link.URL("@vozko_bot"); got != "https://t.me/vozko_bot?start=abc123" {
		t.Errorf("URL = %q", got)
	}
	if got := link.URL("vozko_bot"); got != "https://t.me/vozko_bot?start=abc123" {
		t.Errorf("URL with bare username = %q", got)
	}

	now := time.Now().UTC()
	if link.Expired(now) {
		t.Error("a link with no expiry never expires, correct for a printed QR code")
	}

	past := now.Add(-time.Hour)
	link.ExpiresAt = &past
	if !link.Expired(now) {
		t.Error("a past expiry must expire")
	}
}

func TestValidateWebhookBaseURL(t *testing.T) {
	valid := []string{
		"https://api.example.com",
		"https://api.example.com:8443",
		"https://api.example.com:443",
		"http://localhost:3000",
	}
	for _, raw := range valid {
		if err := ValidateWebhookBaseURL(raw); err != nil {
			t.Errorf("ValidateWebhookBaseURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := map[string]string{
		"":                             "empty",
		"api.example.com":              "no scheme",
		"http://api.example.com":       "plain http is refused outright by Telegram",
		"https://api.example.com:9000": "unsupported port, Telegram silently never delivers",
		"https://api.example.com/hook": "path is owned by the code",
		"https://api.example.com?a=b":  "query string",
	}
	for raw, why := range invalid {
		if err := ValidateWebhookBaseURL(raw); err == nil {
			t.Errorf("ValidateWebhookBaseURL(%q) accepted; %s", raw, why)
		}
	}
}

func TestWebhookURLFor(t *testing.T) {
	got := WebhookURLFor("https://api.example.com/", "acct-1")
	want := "https://api.example.com/webhooks/telegram/acct-1"
	if got != want {
		t.Errorf("WebhookURLFor = %q, want %q", got, want)
	}
}

func TestGenerateWebhookSecret(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		secret, err := GenerateWebhookSecret()
		if err != nil {
			t.Fatalf("GenerateWebhookSecret: %v", err)
		}
		if len(secret) < 1 || len(secret) > 256 {
			t.Fatalf("secret length %d is outside Telegram's 1-256 range", len(secret))
		}
		for _, r := range secret {
			if !strings.ContainsRune(
				"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", r) {
				t.Fatalf("secret %q contains %q, outside Telegram's allowed set", secret, r)
			}
		}
		if seen[secret] {
			t.Fatal("two generated secrets collided")
		}
		seen[secret] = true
	}
}

func TestGenerateDeepLinkTokenIsAcceptable(t *testing.T) {
	for i := 0; i < 50; i++ {
		token, err := GenerateDeepLinkToken()
		if err != nil {
			t.Fatalf("GenerateDeepLinkToken: %v", err)
		}
		if !ValidDeepLinkToken(token) {
			t.Fatalf("generated token %q would be silently dropped by Telegram", token)
		}
	}
}

func TestAPIErrorClassification(t *testing.T) {
	flood := &APIError{Code: 429, RetryAfter: 7}
	if !flood.Retryable() {
		t.Error("429 is retryable")
	}
	if flood.RetryDelay() != 7*time.Second {
		t.Errorf("RetryDelay = %v, want 7s from retry_after", flood.RetryDelay())
	}

	server := &APIError{HTTPStatus: 502, Code: 502}
	if !server.Retryable() {
		t.Error("5xx is retryable")
	}

	dead := &APIError{Code: 401, Description: "Unauthorized"}
	if !dead.NeedsReconnect() || dead.Retryable() {
		t.Error("401 needs a reconnect and must not be retried")
	}

	for _, desc := range []string{
		"Forbidden: bot was blocked by the user",
		"Forbidden: user is deactivated",
		"Forbidden: bot can't initiate conversation with a user",
		"Forbidden: bot was kicked from the supergroup chat",
	} {
		e := &APIError{Code: 403, Description: desc}
		if !e.BlockedByUser() {
			t.Errorf("%q must classify as unreachable", desc)
		}
		if e.Retryable() {
			t.Errorf("%q must not be retried", desc)
		}
	}

	migrated := &APIError{Code: 400, MigrateToChatID: -1001234567890}
	if !migrated.Migrated() {
		t.Error("migrate_to_chat_id must be surfaced")
	}

	var nilErr *APIError
	if nilErr.Retryable() || nilErr.NeedsReconnect() || nilErr.BlockedByUser() || nilErr.Migrated() {
		t.Error("a nil APIError must classify as nothing")
	}
}

func TestDescriptorUsesRuneLimit(t *testing.T) {
	caps := Descriptor().Capabilities

	if caps.MaxTextRunes != MaxTextRunes {
		t.Errorf("MaxTextRunes = %d, want %d", caps.MaxTextRunes, MaxTextRunes)
	}
	if caps.MaxTextBytes != 0 {
		t.Error("Telegram counts characters, not bytes; setting both would double-bound the text")
	}

	emoji := strings.Repeat("😀", MaxTextRunes)
	if caps.TextTooLong(emoji) {
		t.Error("exactly 4096 characters must be accepted regardless of byte length")
	}
	if !caps.TextTooLong(emoji + "x") {
		t.Error("4097 characters must be rejected")
	}
}

func TestDescriptorDeclaresNoWindow(t *testing.T) {
	caps := Descriptor().Capabilities
	if caps.OutboundWindow != 0 {
		t.Error("bot mode has no messaging window; the business-mode window is resolved per account")
	}
	if caps.CanInitiateConversation {
		t.Error("a bot cannot message a user who never started it")
	}
	if caps.SupportsReadReceipts {
		t.Error("Telegram has no delivery or read callbacks; promising them would render a status that never fills")
	}
}

func TestDocumentLimitAcceptsAnyType(t *testing.T) {
	limit := Descriptor().Capabilities.MediaLimits["document"]
	for _, mime := range []string{"application/pdf", "application/zip", "text/csv", ""} {
		if !limit.Allows(mime) {
			t.Errorf("document limit rejected %q; Telegram accepts any document type", mime)
		}
	}
}

func TestAllowedUpdatesOmitsMessageReaction(t *testing.T) {
	for _, u := range AllowedUpdates() {
		if u == "message_reaction" || u == "message_reaction_count" {
			t.Fatalf("%q must not be subscribed: private chats have no administrators", u)
		}
	}

	required := []string{"message", "my_chat_member", "business_connection", "business_message"}
	have := map[string]bool{}
	for _, u := range AllowedUpdates() {
		have[u] = true
	}
	for _, r := range required {
		if !have[r] {
			t.Errorf("allowed_updates is missing %q", r)
		}
	}
}
