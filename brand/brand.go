package brand

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

type Brand struct {
	Key           string
	Name          string
	AIName        string
	AIAliasPrefix string
	LegalName     string
	CNPJ          string
	SiteURL       string
	EmailDomain   string
	SupportEmail  string
	ContactEmail  string
	DPOEmail      string
	Phone         string
	FromEmail     string
	LogoURL       string
}

var (
	mu     sync.RWMutex
	active *Brand
)

func MustLoad() {
	b, err := fromEnv()
	if err != nil {
		panic("brand: " + err.Error())
	}
	mu.Lock()
	active = &b
	mu.Unlock()
}

func Active() Brand {
	mu.RLock()
	a := active
	mu.RUnlock()
	if a != nil {
		return *a
	}
	if b, err := fromEnv(); err == nil {
		mu.Lock()
		active = &b
		mu.Unlock()
		return b
	}
	return Brand{}
}

func AliasPrefix() string {
	mu.RLock()
	defer mu.RUnlock()
	if active == nil {
		return ""
	}
	return active.AIAliasPrefix
}

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
