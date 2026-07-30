package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"vozko/brand"
)

type Config struct {
	AppEnv string

	CORSTrustedOrigins []string
	CookieDomain       string

	AuthJWTSecret      string
	AuthJWTAccessTTL   time.Duration
	AuthJWTRefreshTTL  time.Duration
	ReadMeAuthTokenTTL time.Duration

	RedisAddr     string
	RedisPassword string

	RabbitMQUsername string
	RabbitMQPassword string

	OpenRouterAPIKey       string
	OpenRouterDefaultModel string
	OpenRouterHTTPReferer  string
	OpenRouterXTitle       string

	OllamaURL string

	AsaasAPIKey         string
	AsaasBaseURL        string
	AsaasWebhookToken   string
	ReadMeWebhookSecret string

	// Resend is the sole transactional email provider (auth, orders, tickets,
	// workspace invites, payment receipts). ResendAPIKey is required at boot.
	// The workflow "Send Email" node keeps its own per-call SMTP config and is
	// unrelated to these values.
	ResendAPIKey    string
	ResendFromEmail string
	ResendFromName  string
	// ResendMaxRPS throttles outbound sends client-side to stay under Resend's
	// per-team rate limit (5 req/s by default; raisable on request). Bursts
	// beyond this queue rather than 429.
	ResendMaxRPS int

	RecordingsDir string

	WhisperModel string

	WhatsAppPhoneNumberID      string
	WhatsAppAccessToken        string
	WhatsAppAPIBaseURL         string
	WhatsAppAppID              string
	WhatsAppWebhookVerifyToken string
	MetaAppSecret              string
	MetaConfigID               string
	// MetaAppSecretsExtra are ADDITIONAL app secrets accepted only for inbound webhook
	// signature verification (X-Hub-Signature-256). Meta signs each webhook with the secret
	// of the app the WABA is subscribed to, so while numbers span more than one app (e.g. an
	// app migration) the verifier must accept any of them. Token exchange still uses
	// META_APP_SECRET only. Comma-separated via META_APP_SECRETS.
	MetaAppSecretsExtra []string

	// Instagram (Business Login for Instagram). The Instagram API setup nests
	// inside the same Meta app as WhatsApp but carries its OWN app id and app
	// secret — using the Facebook ones here fails. Webhooks are signed with the
	// Instagram app secret, which is why it is also fed into the inbound
	// signature verifier's accepted-secret list.
	//
	// The three OAuth hosts are fixed by the login path and live as constants in
	// infra/instagram, not here. The scope list is likewise code (see
	// instagram.RequiredScopes): it is a contract with the implementation and with
	// what was submitted for App Review, so a deployment must not be able to
	// silently drop a permission.
	//
	// InstagramRedirectURI is NOT caller-supplied — in OAuth the redirect URI is
	// the security boundary, and it must match the App Dashboard exactly. Only the
	// Graph version is genuinely deployment-owned, because Meta sunsets versions on
	// published dates and it must be bumpable without a code deploy.
	//
	// These are REQUIRED, exactly like the WhatsApp equivalents: Instagram is a
	// first-class channel, and a half-configured deployment that silently serves
	// 404s on every Instagram route is far worse than failing fast at boot.
	InstagramAppID              string
	InstagramAppSecret          string
	InstagramRedirectURI        string
	InstagramWebhookVerifyToken string
	InstagramGraphVersion       string
	// FrontendBaseURL is where OAuth callbacks send the browser back to.
	FrontendBaseURL string

	// 360dialog partner (BSP) integration. New onboarding goes through 360dialog;
	// the reconciliation backstop activates when Dialog360PartnerAPIKey is set.
	Dialog360PartnerID          string
	Dialog360PartnerAPIKey      string
	Dialog360PartnerAPIBase     string
	Dialog360MessagingBase      string
	Dialog360PartnerRedirectURL string
	Dialog360WebhookSecret      string
	// Dialog360WebhookBaseURL is our public base (e.g. https://api.example.com).
	// When set, the inbound messaging webhook is registered on each channel as
	// {base}/webhooks/360dialog/messages?secret={Dialog360WebhookSecret}. Empty
	// relies on the partner Hub default webhook.
	Dialog360WebhookBaseURL string
	// Dialog360SolutionID is the Meta Multi-Partner Solution id (Tech Provider +
	// 360dialog). It is required by account_sharing/numbers to share a WABA.
	Dialog360SolutionID string
	// Dialog360OnboardingEnabled selects the Embedded Signup onboarding path.
	// false (default) => native Meta path: the callback exchanges the code for a
	// Graph token and registers the number directly on the Cloud API.
	// true => 360dialog Partner-Hosted handover. Env: ENABLE_360DIALOG_ONBOARDING.
	Dialog360OnboardingEnabled bool

	PrometheusURL string

	MetricsListenAddr string

	SIPTrunkPortStart    int
	SIPTrunkPortCount    int
	SIPTrunkRTPPortStart int
	SIPTrunkRTPPortEnd   int
	// SIPTrunkMaxPerWorkspace caps how many trunks a non-admin workspace may
	// own (self-service). Admins are never capped. <= 0 disables the cap.
	SIPTrunkMaxPerWorkspace int

	// SIPRealm is the fixed digest realm branch credentials (HA1) are derived
	// under. Keep it stable: changing it invalidates every phone's saved
	// credential workspace-wide. (The per-workspace branch cap is a plan
	// entitlement, PlanDefinition.MaxBranches, not a static config value.)
	SIPRealm string

	// Branch (branch) SIP registrar. Single-VPS model: one process owns all
	// registrations + media. The registrar AUTO-ENABLES when PublicSIPHost is set
	// (the reachable public IP advertised to phones, pinned so STUN is skipped); it
	// stays off in dev/CI where no public host is configured. BranchSIPListenPort is
	// the fixed public UDP port phones register to (distinct from the ephemeral
	// trunk port pool).
	PublicSIPHost       string
	BranchSIPListenPort int

	WhatsAppStunServers []string

	WhatsAppMediaUDPMuxPort int

	RecordingsStagingDir   string
	RecordingUploadWorkers int

	PublicReplicaURL string

	ReplicaID string

	CFAccountID   string
	CFKVNamespace string
	CFKVAPIToken  string
	CFKVKeyPrefix string

	MCPVaultKey             string
	MCPVaultKEKVersion      int
	MCPStateSignerKey       string
	MCPCallbackURL          string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string

	ShortLinkBaseURL            string
	ShortLinkCodeLength         int
	ShortLinkClickRetentionDays int
	ShortLinkIPHashSalt         string
	GoogleSafeBrowsingAPIKey    string
}

func LoadConfig() Config {
	// The app ships no brand of its own: fail fast at boot if the BRAND_* identity
	// is not fully provided via env (white-label; see package brand).
	brand.MustLoad()

	return Config{

		AppEnv: os.Getenv("APP_ENV"),

		CORSTrustedOrigins: splitTrimmed(mustGetEnv("CORS_TRUSTED_ORIGINS")),
		CookieDomain:       mustGetEnvTrimmed("COOKIE_DOMAIN"),

		AuthJWTSecret:      mustGetEnv("AUTH_JWT_SECRET"),
		AuthJWTAccessTTL:   getDurationEnv("AUTH_JWT_ACCESS_TTL", 15*time.Minute),
		AuthJWTRefreshTTL:  getDurationEnv("AUTH_JWT_REFRESH_TTL", 720*time.Hour),
		ReadMeAuthTokenTTL: getDurationEnv("README_AUTH_TOKEN_TTL", 8*time.Hour),

		RedisAddr:     mustGetEnv("REDIS_ADDR"),
		RedisPassword: mustGetEnv("REDIS_PASSWORD"),

		RabbitMQUsername: os.Getenv("RABBITMQ_USERNAME"),
		RabbitMQPassword: os.Getenv("RABBITMQ_PASSWORD"),

		OpenRouterAPIKey:       mustGetEnvTrimmed("OPENROUTER_API_KEY"),
		OpenRouterDefaultModel: getEnvTrimmed("OPENROUTER_DEFAULT_MODEL", "openai/gpt-4o"),
		OpenRouterHTTPReferer:  trimEnv("OPENROUTER_HTTP_REFERER"),
		OpenRouterXTitle:       trimEnv("OPENROUTER_X_TITLE"),

		OllamaURL: getEnvTrimmed("OLLAMA_URL", "http://localhost:11434"),

		AsaasAPIKey:         mustGetEnv("ASAAS_API_KEY"),
		AsaasBaseURL:        mustGetEnv("ASAAS_BASE_URL"),
		AsaasWebhookToken:   mustGetEnv("ASAAS_WEBHOOK_TOKEN"),
		ReadMeWebhookSecret: trimEnv("README_WEBHOOK_SECRET"),

		ResendAPIKey:    mustGetEnvTrimmed("RESEND_API_KEY"),
		ResendFromEmail: getEnvTrimmed("RESEND_FROM_EMAIL", brand.Active().FromEmail),
		ResendFromName:  getEnvTrimmed("RESEND_FROM_NAME", brand.Active().Name),
		ResendMaxRPS:    getIntEnv("RESEND_MAX_REQUESTS_PER_SECOND", 4),

		RecordingsDir: getEnv("RECORDINGS_DIR", "/recordings"),

		WhisperModel: getEnv("WHISPER_MODEL", "ggml-large-v3-turbo"),

		WhatsAppPhoneNumberID:      trimEnv("WHATSAPP_BUSINESS_PHONE_NUMBER_ID"),
		WhatsAppAccessToken:        trimEnv("WHATSAPP_ACCESS_TOKEN"),
		WhatsAppAPIBaseURL:         trimEnv("WHATSAPP_API_BASE_URL"),
		WhatsAppAppID:              mustGetEnvTrimmed("WHATSAPP_APP_ID"),
		WhatsAppWebhookVerifyToken: mustGetEnvTrimmed("WHATSAPP_WEBHOOK_VERIFY_TOKEN"),
		MetaAppSecret:              mustGetEnvTrimmed("META_APP_SECRET"),
		MetaConfigID:               mustGetEnvTrimmed("META_CONFIG_ID"),
		MetaAppSecretsExtra:        parseCSVEnv("META_APP_SECRETS"),

		InstagramAppID:              mustGetEnvTrimmed("INSTAGRAM_APP_ID"),
		InstagramAppSecret:          mustGetEnvTrimmed("INSTAGRAM_APP_SECRET"),
		InstagramRedirectURI:        mustGetEnvTrimmed("INSTAGRAM_REDIRECT_URI"),
		InstagramWebhookVerifyToken: mustGetEnvTrimmed("INSTAGRAM_WEBHOOK_VERIFY_TOKEN"),
		// Empty means "use the client default", so a deployment only pins a version
		// when it needs to.
		InstagramGraphVersion: trimEnv("INSTAGRAM_GRAPH_VERSION"),
		FrontendBaseURL:       strings.TrimRight(trimEnv("FRONTEND_URL"), "/"),

		GoogleOAuthClientID:     trimEnv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret: trimEnv("GOOGLE_OAUTH_CLIENT_SECRET"),

		Dialog360PartnerID:          mustGetEnvTrimmed("D360_PARTNER_ID"),
		Dialog360PartnerAPIKey:      mustGetEnvTrimmed("D360_PARTNER_API_KEY"),
		Dialog360PartnerAPIBase:     getEnvTrimmed("D360_PARTNER_API_BASE_URL", "https://hub.360dialog.io/api/v2"),
		Dialog360MessagingBase:      getEnvTrimmed("D360_MESSAGING_BASE_URL", "https://waba-v2.360dialog.io"),
		Dialog360PartnerRedirectURL: trimEnv("D360_PARTNER_REDIRECT_URL"),
		Dialog360WebhookSecret:      mustGetEnvTrimmed("D360_WEBHOOK_SECRET"),
		Dialog360WebhookBaseURL:     mustGetEnvTrimmed("D360_WEBHOOK_BASE_URL"),
		Dialog360SolutionID:         mustGetEnvTrimmed("D360_SOLUTION_ID"),
		Dialog360OnboardingEnabled:  getBoolEnv("ENABLE_360DIALOG_ONBOARDING", false),

		PrometheusURL:     getEnvTrimmed("PROMETHEUS_URL", "http://localhost:9090"),
		MetricsListenAddr: getEnvTrimmed("METRICS_LISTEN_ADDR", ":9213"),

		SIPTrunkPortStart: getIntEnv("SIP_TRUNK_PORT_START", 15060),
		SIPTrunkPortCount: getIntEnv("SIP_TRUNK_PORT_COUNT", 100),
		// RTP window sits ENTIRELY below the Linux ephemeral floor (32768) and starts
		// even (RFC 3550: RTP even, RTCP = RTP+1). 16384-32767 = 8191 pairs, disjoint
		// from the SIP trunk range above. The boot-time port guard refuses to start if
		// this ever overlaps the OS ephemeral range or the SIP range.
		SIPTrunkRTPPortStart:    getIntEnv("SIP_TRUNK_RTP_PORT_START", 16384),
		SIPTrunkRTPPortEnd:      getIntEnv("SIP_TRUNK_RTP_PORT_END", 32767),
		SIPTrunkMaxPerWorkspace: getIntEnv("SIP_TRUNK_MAX_PER_WORKSPACE", 20),
		SIPRealm:                getEnv("SIP_REALM", "vozko"),
		PublicSIPHost:           getEnvTrimmed("PUBLIC_SIP_HOST", ""),
		BranchSIPListenPort:     getIntEnv("BRANCH_SIP_LISTEN_PORT", 5070),

		WhatsAppStunServers:     parseCSVEnv("WHATSAPP_STUN_SERVERS"),
		WhatsAppMediaUDPMuxPort: mustGetIntEnv("WHATSAPP_MEDIA_UDP_MUX_PORT"),

		RecordingsStagingDir:   getEnvTrimmed("RECORDINGS_STAGING_DIR", ""),
		RecordingUploadWorkers: getIntEnv("RECORDING_UPLOAD_WORKERS", 8),

		PublicReplicaURL: trimEnv("REPLICA_PUBLIC_URL"),

		ReplicaID: mustGetEnvTrimmed("REPLICA_ID"),

		CFAccountID:   mustGetEnvTrimmed("CLOUDFLARE_ACCOUNT_ID"),
		CFKVNamespace: mustGetEnvTrimmed("CLOUDFLARE_KV_NAMESPACE_ID"),
		CFKVAPIToken:  mustGetEnvTrimmed("CLOUDFLARE_KV_API_TOKEN"),
		CFKVKeyPrefix: getEnvTrimmed("CLOUDFLARE_KV_KEY_PREFIX", "replicas:"),

		MCPVaultKey:        trimEnv("MCP_VAULT_KEY"),
		MCPVaultKEKVersion: getIntEnv("MCP_VAULT_KEK_VERSION", 1),
		MCPStateSignerKey:  trimEnv("MCP_STATE_SIGNER_KEY"),
		MCPCallbackURL:     getEnvTrimmed("MCP_OAUTH_CALLBACK_URL", "http://localhost:4000/mcp/oauth/callback"),

		ShortLinkBaseURL:            mustGetEnvTrimmed("SHORTLINK_BASE_URL"),
		ShortLinkCodeLength:         getIntEnv("SHORTLINK_CODE_LENGTH", 7),
		ShortLinkClickRetentionDays: getIntEnv("SHORTLINK_CLICK_RETENTION_DAYS", 400),
		ShortLinkIPHashSalt:         mustGetEnvTrimmed("SHORTLINK_IP_HASH_SALT"),
		GoogleSafeBrowsingAPIKey:    trimEnv("GOOGLE_SAFE_BROWSING_API_KEY"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvTrimmed(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func trimEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getBoolEnv(key string, fallback bool) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func mustGetEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return val
}

func mustGetEnvTrimmed(key string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return val
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Fatalf("invalid duration for %s: %v", key, err)
	}
	return duration
}

func optionalDurationEnv(key string) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("invalid duration for %s: %v", key, err)
		return 0
	}
	return duration
}

func getIntEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid integer for %s: %v", key, err)
	}
	return val
}

func mustGetIntEnv(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		log.Fatalf("missing required environment variable: %s", key)
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid integer for %s: %v", key, err)
	}
	return val
}

func getFloatEnv(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		log.Fatalf("invalid float for %s: %v", key, err)
	}
	return val
}

func splitTrimmed(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
