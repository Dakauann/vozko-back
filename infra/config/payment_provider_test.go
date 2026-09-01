package config

import (
	"os"
	"testing"

	"vozko/domain/payment"
)

func TestSandboxPayerEmail_HonouredOnlyInDevelopment(t *testing.T) {
	t.Setenv("MERCADOPAGO_SANDBOX_PAYER_EMAIL", "test_user_4138@testuser.com")

	t.Setenv("APP_ENV", "development")
	if got := sandboxPayerEmail(); got != "test_user_4138@testuser.com" {
		t.Fatalf("development must honour the override, got %q", got)
	}

	// Anything that is not development must ignore it, so a stray line in a production
	// environment file cannot redirect who a real charge is addressed to.
	for _, env := range []string{"production", "staging", "homolog", "", "Development"} {
		t.Setenv("APP_ENV", env)
		if got := sandboxPayerEmail(); got != "" {
			t.Fatalf("APP_ENV=%q must ignore the override, got %q", env, got)
		}
	}
}

func TestSandboxPayerEmail_EmptyWhenUnset(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	_ = os.Unsetenv("MERCADOPAGO_SANDBOX_PAYER_EMAIL")
	if got := sandboxPayerEmail(); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	t.Setenv("MERCADOPAGO_SANDBOX_PAYER_EMAIL", "   ")
	if got := sandboxPayerEmail(); got != "" {
		t.Fatalf("whitespace must count as unset, got %q", got)
	}
}

func TestParseProviderIsWiredIntoConfig(t *testing.T) {
	// Guards the switch itself: an unrecognized provider must not silently fall back to
	// Asaas and bill through the wrong gateway.
	for _, raw := range []string{"", "asaas", "ASAAS", " Asaas "} {
		if p, err := parseProviderForTest(raw); err != nil || string(p) != "asaas" {
			t.Fatalf("%q should resolve to asaas, got (%v,%v)", raw, p, err)
		}
	}
	for _, raw := range []string{"mercadopago", "mercado_pago", "mercado-pago", "MP", "Mercado Pago"} {
		if p, err := parseProviderForTest(raw); err != nil || string(p) != "mercadopago" {
			t.Fatalf("%q should resolve to mercadopago, got (%v,%v)", raw, p, err)
		}
	}
	for _, raw := range []string{"stripe", "pagseguro", "asaaas"} {
		if _, err := parseProviderForTest(raw); err == nil {
			t.Fatalf("%q must be rejected rather than defaulting", raw)
		}
	}
}

// parseProviderForTest keeps the domain import out of the test's assertions above.
func parseProviderForTest(raw string) (payment.Provider, error) { return payment.ParseProvider(raw) }

func TestSandboxPayerStatus_HonouredOnlyInDevelopment(t *testing.T) {
	t.Setenv("MERCADOPAGO_SANDBOX_PAYER_STATUS", "APRO")

	t.Setenv("APP_ENV", "development")
	if got := sandboxPayerStatus(); got != "APRO" {
		t.Fatalf("development must honour the override, got %q", got)
	}

	for _, env := range []string{"production", "staging", ""} {
		t.Setenv("APP_ENV", env)
		if got := sandboxPayerStatus(); got != "" {
			t.Fatalf("APP_ENV=%q must ignore the override, got %q", env, got)
		}
	}
}

func TestSandboxPayerStatus_NormalizesAndDefaultsEmpty(t *testing.T) {
	t.Setenv("APP_ENV", "development")

	t.Setenv("MERCADOPAGO_SANDBOX_PAYER_STATUS", "  apro ")
	if got := sandboxPayerStatus(); got != "APRO" {
		t.Fatalf("expected APRO, got %q", got)
	}
	for _, valid := range []string{"CONT", "OTHE"} {
		t.Setenv("MERCADOPAGO_SANDBOX_PAYER_STATUS", valid)
		if got := sandboxPayerStatus(); got != valid {
			t.Fatalf("expected %q, got %q", valid, got)
		}
	}

	t.Setenv("MERCADOPAGO_SANDBOX_PAYER_STATUS", "   ")
	if got := sandboxPayerStatus(); got != "" {
		t.Fatalf("whitespace must count as unset, got %q", got)
	}
}
