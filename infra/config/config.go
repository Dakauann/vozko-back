package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/payment"
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

	PaymentProvider payment.Provider

	AsaasAPIKey       string
	AsaasBaseURL      string
	AsaasWebhookToken string

	MercadoPagoAccessToken        string
	MercadoPagoWebhookSecret      string
	MercadoPagoBaseURL            string
	MercadoPagoNotificationURL    string
	MercadoPagoSignatureTolerance time.Duration
	MercadoPagoSandboxPayerEmail  string
	MercadoPagoSandboxPayerStatus string

	ReadMeWebhookSecret string

	ResendAPIKey    string
	ResendFromEmail string
	ResendFromName  string
	ResendMaxRPS    int

	RecordingsDir string

	WhisperModel string

	WhatsAppPhoneNumberID      string
	WhatsAppAccessToken        string
	WhatsAppAPIBaseURL         string
	WhatsAppAppID              string
	WhatsAppWebhookVerifyToken string
	MetaAppSecret              string
	MetaConfigID               string
	MetaAppSecretsExtra        []string

	InstagramAppID              string
	InstagramAppSecret          string
	InstagramRedirectURI        string
	InstagramWebhookVerifyToken string
	InstagramGraphVersion       string
	FrontendBaseURL             string

	TelegramWebhookBaseURL string
	TelegramBotAPIBaseURL  string

	UnofficialWhatsAppWebhookBaseURL string
	UnofficialWhatsAppServerURL      string
	UnofficialWhatsAppAdminToken     string
	UnofficialWhatsAppServerName     string
	UnofficialWhatsAppMaxSessions    int

	Dialog360PartnerID          string
	Dialog360PartnerAPIKey      string
	Dialog360PartnerAPIBase     string
	Dialog360MessagingBase      string
	Dialog360PartnerRedirectURL string
	Dialog360WebhookSecret      string
	Dialog360WebhookBaseURL     string
	Dialog360SolutionID         string
	Dialog360OnboardingEnabled  bool

	PrometheusURL string

	MetricsListenAddr string

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
	brand.MustLoad()

	provider, asaasAPIKey, asaasBaseURL, asaasWebhookToken,
		mpAccessToken, mpWebhookSecret, mpNotificationURL := loadPaymentProvider()

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

		PaymentProvider: provider,

		AsaasAPIKey:       asaasAPIKey,
		AsaasBaseURL:      asaasBaseURL,
		AsaasWebhookToken: asaasWebhookToken,

		MercadoPagoAccessToken:        mpAccessToken,
		MercadoPagoWebhookSecret:      mpWebhookSecret,
		MercadoPagoBaseURL:            trimEnv("MERCADOPAGO_BASE_URL"),
		MercadoPagoNotificationURL:    mpNotificationURL,
		MercadoPagoSignatureTolerance: optionalDurationEnv("MERCADOPAGO_SIGNATURE_TOLERANCE"),
		MercadoPagoSandboxPayerEmail:  sandboxPayerEmail(),
		MercadoPagoSandboxPayerStatus: sandboxPayerStatus(),

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
		InstagramGraphVersion:       trimEnv("INSTAGRAM_GRAPH_VERSION"),
		FrontendBaseURL:             strings.TrimRight(trimEnv("FRONTEND_URL"), "/"),

		TelegramWebhookBaseURL: strings.TrimRight(mustGetEnvTrimmed("TELEGRAM_WEBHOOK_BASE_URL"), "/"),
		TelegramBotAPIBaseURL:  strings.TrimRight(trimEnv("TELEGRAM_BOT_API_BASE_URL"), "/"),

		UnofficialWhatsAppWebhookBaseURL: strings.TrimRight(
			mustGetEnvTrimmed("UNOFFICIAL_WHATSAPP_WEBHOOK_BASE_URL"), "/"),
		UnofficialWhatsAppServerURL: strings.TrimRight(
			mustGetEnvTrimmed("UNOFFICIAL_WHATSAPP_SERVER_URL"), "/"),
		UnofficialWhatsAppAdminToken:  mustGetEnvTrimmed("UNOFFICIAL_WHATSAPP_ADMIN_TOKEN"),
		UnofficialWhatsAppServerName:  getEnvTrimmed("UNOFFICIAL_WHATSAPP_SERVER_NAME", "platform"),
		UnofficialWhatsAppMaxSessions: mustGetEnvInt("UNOFFICIAL_WHATSAPP_MAX_SESSIONS"),

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

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid integer for %s: %v", key, err)
	}
	return parsed
}

func mustGetEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		log.Fatalf("invalid integer for %s: %v", key, err)
	}
	return parsed
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

func loadPaymentProvider() (
	provider payment.Provider,
	asaasAPIKey, asaasBaseURL, asaasWebhookToken string,
	mpAccessToken, mpWebhookSecret, mpNotificationURL string,
) {
	raw := trimEnv("PAYMENT_PROVIDER")
	provider, err := payment.ParseProvider(raw)
	if err != nil {
		log.Fatalf("invalid PAYMENT_PROVIDER %q: must be one of %q, %q", raw, payment.ProviderAsaas, payment.ProviderMercadoPago)
	}

	switch provider {
	case payment.ProviderMercadoPago:
		mpAccessToken = mustGetEnvTrimmed("MERCADOPAGO_ACCESS_TOKEN")
		mpWebhookSecret = mustGetEnvTrimmed("MERCADOPAGO_WEBHOOK_SECRET")
		mpNotificationURL = strings.TrimRight(mustGetEnvTrimmed("MERCADOPAGO_NOTIFICATION_URL"), "/")
		asaasAPIKey = trimEnv("ASAAS_API_KEY")
		asaasBaseURL = trimEnv("ASAAS_BASE_URL")
		asaasWebhookToken = trimEnv("ASAAS_WEBHOOK_TOKEN")

	default:
		asaasAPIKey = mustGetEnv("ASAAS_API_KEY")
		asaasBaseURL = mustGetEnv("ASAAS_BASE_URL")
		asaasWebhookToken = mustGetEnv("ASAAS_WEBHOOK_TOKEN")
		mpAccessToken = trimEnv("MERCADOPAGO_ACCESS_TOKEN")
		mpWebhookSecret = trimEnv("MERCADOPAGO_WEBHOOK_SECRET")
		mpNotificationURL = strings.TrimRight(trimEnv("MERCADOPAGO_NOTIFICATION_URL"), "/")
	}

	log.Printf("[config] payment provider: %s", provider)
	return provider, asaasAPIKey, asaasBaseURL, asaasWebhookToken, mpAccessToken, mpWebhookSecret, mpNotificationURL
}

func sandboxPayerEmail() string {
	email := trimEnv("MERCADOPAGO_SANDBOX_PAYER_EMAIL")
	if email == "" {
		return ""
	}
	if os.Getenv("APP_ENV") != "development" {
		log.Printf("[config] IGNORING MERCADOPAGO_SANDBOX_PAYER_EMAIL: it is honoured only when APP_ENV=development")
		return ""
	}
	log.Printf("[config] Mercado Pago sandbox payer override active: every charge will be addressed to %s", email)
	return email
}

func sandboxPayerStatus() string {
	status := strings.ToUpper(trimEnv("MERCADOPAGO_SANDBOX_PAYER_STATUS"))
	if status == "" {
		return ""
	}
	if os.Getenv("APP_ENV") != "development" {
		log.Printf("[config] IGNORING MERCADOPAGO_SANDBOX_PAYER_STATUS: it is honoured only when APP_ENV=development")
		return ""
	}
	switch status {
	case "APRO", "CONT", "OTHE":
		log.Printf("[config] Mercado Pago sandbox status override active: every charge forced to %s", status)
		return status
	default:
		log.Fatalf("invalid MERCADOPAGO_SANDBOX_PAYER_STATUS %q: must be APRO (approved), CONT (pending) or OTHE (rejected)", status)
		return ""
	}
}
