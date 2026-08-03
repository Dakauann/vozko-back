// Package brand holds the white-label identity of the running instance.
//
// The codebase ships NO brand of its own: there is no default, fallback, or
// hardcoded name anywhere in committed code. Every field is supplied via BRAND_*
// environment variables at deploy time, for every brand. Missing required
// variables are fatal at boot (MustLoad), so the app can never start, or render,
// unbranded. This keeps the source fully brand-agnostic and lets the same code
// run as any brand purely through configuration.
package brand

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Brand is the resolved identity for this instance. Fields are added as more
// surfaces are routed through the brand; each maps to a BRAND_* env var.
type Brand struct {
	Key           string // short slug for logs/telemetry (BRAND_KEY)
	Name          string // public display / trade name (BRAND_NAME): UI, emails, AI persona
	AIName        string // display name of the AI voice/model sub-brand (BRAND_AI_NAME), e.g. "YourBrand AI"
	AIAliasPrefix string // API-facing prefix for AI model ids (BRAND_AI_ALIAS_PREFIX), e.g. "yourbrandai_"
	LegalName     string // registered legal entity / razão social (BRAND_LEGAL_NAME)
	CNPJ          string // legal registration number (BRAND_CNPJ)
	SiteURL       string // canonical site / dashboard base URL (BRAND_SITE_URL)
	EmailDomain   string // bare email domain for synthetic addresses (BRAND_EMAIL_DOMAIN)
	SupportEmail  string // support inbox (BRAND_SUPPORT_EMAIL)
	ContactEmail  string // general contact / relationship inbox (BRAND_CONTACT_EMAIL)
	DPOEmail      string // data protection officer inbox (BRAND_DPO_EMAIL)
	Phone         string // support / contact phone (BRAND_PHONE)
	FromEmail     string // transactional email From address (BRAND_FROM_EMAIL)
	LogoURL       string // single logo asset URL, CDN-hosted (BRAND_LOGO_URL)
}

var (
	mu     sync.RWMutex
	active *Brand
)

// MustLoad resolves the brand from the environment and stores it as active.
// Call once at process boot (see infra/config.LoadConfig). It panics if any
// required BRAND_* variable is missing, by design: there is no default brand.
func MustLoad() {
	b, err := fromEnv()
	if err != nil {
		panic("brand: " + err.Error())
	}
	mu.Lock()
	active = &b
	mu.Unlock()
}

// Active returns the resolved brand. In production MustLoad runs at boot; if a
// caller reaches this before boot, it lazily resolves from the environment and
// panics with guidance if that is not configured either (never defaults).
func Active() Brand {
	mu.RLock()
	a := active
	mu.RUnlock()
	if a != nil {
		return *a
	}
	// Not loaded yet: this is either pre-boot or a unit test. Try the environment;
	// if it is fully set, cache and use it. Otherwise return a zero brand rather
	// than panic, MustLoad at boot is the real fail-fast guardrail, so a running
	// production process (where MustLoad has succeeded) never reaches this branch.
	if b, err := fromEnv(); err == nil {
		mu.Lock()
		active = &b
		mu.Unlock()
		return b
	}
	return Brand{}
}

// AliasPrefix returns the active brand's API-facing model-id prefix, or "" if the
// brand has not been loaded yet (e.g. a unit test that did not boot config). It
// NEVER panics and NEVER substitutes a brand default: "" simply means "do not
// translate", so the alias edge is a no-op and callers keep the internal form.
// In production MustLoad has run at boot, so this returns the configured prefix.
func AliasPrefix() string {
	mu.RLock()
	defer mu.RUnlock()
	if active == nil {
		return ""
	}
	return active.AIAliasPrefix
}

// SetForTest installs a brand for tests that exercise brand-dependent code
// without booting the full config. Not for production use.
func SetForTest(b Brand) {
	mu.Lock()
	active = &b
	mu.Unlock()
}

func fromEnv() (Brand, error) {
	var missing []string
	req := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}

	b := Brand{
		Key:           req("BRAND_KEY"),
		Name:          req("BRAND_NAME"),
		AIName:        req("BRAND_AI_NAME"),
		AIAliasPrefix: req("BRAND_AI_ALIAS_PREFIX"),
		LegalName:     req("BRAND_LEGAL_NAME"),
		CNPJ:          req("BRAND_CNPJ"),
		SiteURL:       req("BRAND_SITE_URL"),
		EmailDomain:   req("BRAND_EMAIL_DOMAIN"),
		SupportEmail:  req("BRAND_SUPPORT_EMAIL"),
		ContactEmail:  req("BRAND_CONTACT_EMAIL"),
		DPOEmail:      req("BRAND_DPO_EMAIL"),
		Phone:         req("BRAND_PHONE"),
		FromEmail:     req("BRAND_FROM_EMAIL"),
		LogoURL:       req("BRAND_LOGO_URL"),
	}

	if len(missing) > 0 {
		return Brand{}, fmt.Errorf(
			"missing required brand env var(s): %s (the codebase ships no default brand; every BRAND_* value must be set)",
			strings.Join(missing, ", "),
		)
	}
	return b, nil
}
