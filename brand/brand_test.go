package brand

import (
	"os"
	"strings"
	"testing"
)

var allBrandEnv = map[string]string{
	"BRAND_KEY":             "acme",
	"BRAND_NAME":            "Acme",
	"BRAND_AI_NAME":         "Acme AI",
	"BRAND_AI_ALIAS_PREFIX": "acme_",
	"BRAND_LEGAL_NAME":      "ACME LTDA",
	"BRAND_CNPJ":            "00.000.000/0001-00",
	"BRAND_SITE_URL":        "https://acme.example",
	"BRAND_EMAIL_DOMAIN":    "acme.example",
	"BRAND_SUPPORT_EMAIL":   "suporte@acme.example",
	"BRAND_CONTACT_EMAIL":   "contato@acme.example",
	"BRAND_DPO_EMAIL":       "dpo@acme.example",
	"BRAND_PHONE":           "+00 00 0000-0000",
	"BRAND_FROM_EMAIL":      "no-reply@acme.example",
	"BRAND_LOGO_URL":        "https://cdn.acme.example/logo.png",
	"BRAND_GENDER":          "feminine",
}

func setAllBrandEnv(t *testing.T) {
	for k, v := range allBrandEnv {
		os.Setenv(k, v)
	}
	t.Cleanup(func() {
		for k := range allBrandEnv {
			os.Unsetenv(k)
		}
	})
}

func TestFromEnv_MissingRequiredErrors(t *testing.T) {
	for k := range allBrandEnv {
		os.Unsetenv(k)
	}
	_, err := fromEnv()
	if err == nil || !strings.Contains(err.Error(), "BRAND_NAME") {
		t.Fatalf("expected a missing-required error mentioning BRAND_NAME, got %v", err)
	}
}

func TestFromEnv_LoadsFromEnv(t *testing.T) {
	setAllBrandEnv(t)
	b, err := fromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Name != "Acme" || b.Key != "acme" || b.AIName != "Acme AI" || b.AIAliasPrefix != "acme_" ||
		b.LegalName != "ACME LTDA" || b.SupportEmail != "suporte@acme.example" || b.LogoURL != "https://cdn.acme.example/logo.png" ||
		b.Gender != Feminine {
		t.Fatalf("got %+v", b)
	}
}

func TestFromEnv_MissingGenderRefusesLikeEveryOtherBrandField(t *testing.T) {
	setAllBrandEnv(t)
	os.Unsetenv("BRAND_GENDER")
	_, err := fromEnv()
	if err == nil || !strings.Contains(err.Error(), "BRAND_GENDER") {
		t.Fatalf("expected a missing-required error mentioning BRAND_GENDER, got %v", err)
	}
}

func TestFromEnv_UnknownGenderRefusesInsteadOfPickingOne(t *testing.T) {
	setAllBrandEnv(t)
	os.Setenv("BRAND_GENDER", "neutral")
	_, err := fromEnv()
	if err == nil || !strings.Contains(err.Error(), "BRAND_GENDER") {
		t.Fatalf("expected an invalid BRAND_GENDER error, got %v", err)
	}
}

func TestActive_UsesSetForTest(t *testing.T) {
	SetForTest(Brand{Name: "Test", Key: "test"})
	if got := Active().Name; got != "Test" {
		t.Fatalf("SetForTest not honored, got %q", got)
	}
}

func TestAliasPrefix_ReturnsActive(t *testing.T) {
	SetForTest(Brand{Name: "T", Key: "t", AIName: "T AI", AIAliasPrefix: "acme_"})
	if got := AliasPrefix(); got != "acme_" {
		t.Fatalf("AliasPrefix() = %q, want %q", got, "acme_")
	}
}
