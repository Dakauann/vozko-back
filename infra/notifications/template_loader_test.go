package notification_service

import (
	"strings"
	"testing"

	"vozko/brand"
)

// loaderForTests resolves the real templates dir relative to this package.
func loaderForTests() *TemplateLoaderService {
	return NewTemplateLoaderService("templates")
}

func TestLoadTemplate_ComposesLayoutAndRendersData(t *testing.T) {
	// The layout renders brand chrome from the active brand; set one for the test.
	brand.SetForTest(brand.Brand{
		Name: "Vozko", LegalName: "VOZKO GLOBAL TECNOLOGIA LTDA", CNPJ: "63.819.955/0001-95",
		SiteURL: "https://vozkoglobal.com", EmailDomain: "vozkoglobal.com",
		SupportEmail: "suporte@vozkoglobal.com", LogoURL: "https://cdn.vozkoglobal.com/logo.png",
	})
	t.Cleanup(func() { brand.SetForTest(brand.Brand{}) })

	out, err := loaderForTests().LoadTemplate("password_reset.html", map[string]interface{}{
		"Email":     "user@example.com",
		"Token":     "482913",
		"ExpiresIn": "15 minutos",
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	// Composed into the shared layout (header + footer + shell).
	for _, want := range []string{"<!DOCTYPE html>", "Vozko", "CNPJ", "vozkoglobal.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered email to contain %q", want)
		}
	}
	// Placeholders rendered.
	for _, want := range []string{"user@example.com", "482913", "15 minutos"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered email to contain placeholder value %q", want)
		}
	}
}

func TestLoadTemplate_FollowsDesignSystem(t *testing.T) {
	out, err := loaderForTests().LoadTemplate("password_reset.html", map[string]interface{}{
		"Email": "user@example.com", "Token": "482913", "ExpiresIn": "15 minutos",
	})
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}

	// The one action voice: Signal Blue must be present.
	if !strings.Contains(out, "#2463eb") {
		t.Fatal("expected Signal Blue (#2463eb) in the rendered email")
	}

	// Forbidden patterns (design-system violations) must be gone.
	lower := strings.ToLower(out)
	forbidden := map[string]string{
		"linear-gradient":  "gradients are not allowed in transactional UI",
		"#4a90e2":          "the old non-brand blue must be replaced by Signal Blue",
		"#1a1a1a":          "near-black text must be Ink (#344256)",
		"border-left:4px":  "thick colored left-border accent stripes are forbidden",
		"border-left: 4px": "thick colored left-border accent stripes are forbidden",
	}
	for pat, why := range forbidden {
		if strings.Contains(lower, pat) {
			t.Fatalf("found forbidden design pattern %q: %s", pat, why)
		}
	}

	// No em-dash / en-dash in copy (CLAUDE.md text rule).
	if strings.ContainsRune(out, '—') || strings.ContainsRune(out, '–') {
		t.Fatal("em-dash/en-dash found in email copy; use a single space")
	}
}

func TestNewTemplates_RenderAndFollowDesign(t *testing.T) {
	data := map[string]interface{}{
		"AddonName": "+1 Número WhatsApp", "Amount": "US$ 49,00", "RenewalDate": "10/07/2026",
		"PlanName": "Plano Growth", "ExpiryDate": "12/07/2026", "PhoneNumber": "+55 11 90000-0000",
		"Reason": "Plano expirado", "Balance": "US$ 2,30", "Quality": "VERMELHA", "DisableDate": "30/07/2026",
		"DashboardURL": "https://app.vozkoglobal.com/dashboard", "ResetURL": "https://app.vozkoglobal.com/reset",
		"Email": "user@example.com", "Headline": "Conta verificada", "Subtitle": "tudo certo",
		"Message": "Sua conta foi atualizada.", "StatusLabel": "Aprovado", "Tone": "success", "Glyph": "check",
	}
	templates := []string{
		"addon_renewal_reminder.html", "plan_expiry_reminder.html", "addon_payment_failed.html",
		"whatsapp_number_suspended.html", "low_balance_warning.html", "wallet_topup_confirmed.html",
		"whatsapp_quality_alert.html", "whatsapp_number_banned.html", "whatsapp_scheduled_disable.html",
		"whatsapp_number_live.html", "whatsapp_onboarding_failed.html", "whatsapp_account_update.html",
		"password_changed.html", "login_locked.html",
	}
	loader := loaderForTests()
	forbidden := []string{"linear-gradient", "#4a90e2", "#1a1a1a", "border-left:4px", "border-left: 4px"}
	for _, name := range templates {
		out, err := loader.LoadTemplate(name, data)
		if err != nil {
			t.Fatalf("%s: render error: %v", name, err)
		}
		if !strings.Contains(out, "#2463eb") {
			t.Fatalf("%s: missing Signal Blue", name)
		}
		if !strings.Contains(out, "<!DOCTYPE html>") {
			t.Fatalf("%s: not composed into the shared layout", name)
		}
		lower := strings.ToLower(out)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("%s: forbidden design pattern %q", name, bad)
			}
		}
		if strings.ContainsRune(out, '—') || strings.ContainsRune(out, '–') {
			t.Fatalf("%s: em/en-dash found in copy", name)
		}
	}
}

func TestConvertedExistingTemplates_RenderWithRealSenderData(t *testing.T) {
	loader := loaderForTests()
	cases := []struct {
		name string
		data map[string]interface{}
	}{
		{"password_reset.html", map[string]interface{}{"Email": "u@x.com", "Token": "123456", "ExpiresIn": "1 hora"}},
		{"email_verification.html", map[string]interface{}{"Email": "u@x.com", "Token": "123456"}},
		{"welcome_email.html", map[string]interface{}{"Email": "u@x.com"}},
		{"login_successful.html", map[string]interface{}{"Email": "u@x.com", "LoginTime": "2026-06-28 14:30:00"}},
		{"workspace_invite.html", map[string]interface{}{"Email": "u@x.com", "InviterEmail": "admin@x.com", "WorkspaceName": "Equipe Vendas", "Role": "EDITOR", "AcceptURL": "https://app.vozkoglobal.com/invite?t=abc"}},
		{"issue_created_confirmation.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "IssueDescription": "Tentei conectar e deu erro.", "DashboardURL": "https://app.vozkoglobal.com/dashboard"}},
		{"issue_status_update.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "OldStatusLabel": "Aberto", "NewStatusLabel": "Em andamento", "IsClosed": false, "DashboardURL": "https://app.vozkoglobal.com/dashboard"}},
		{"issue_response_notification.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "StatusLabel": "Em andamento", "AuthorName": "Suporte Vozko", "ResponseDate": "29/06/2026 10:00", "ResponseBody": "Olá, já estamos verificando.", "DashboardURL": "https://app.vozkoglobal.com/dashboard"}},
	}
	forbidden := []string{"linear-gradient", "#4a90e2", "#1a1a1a", "border-left:4px", "border-left: 4px"}
	for _, c := range cases {
		out, err := loader.LoadTemplate(c.name, c.data)
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		// Catches a converted template referencing a placeholder the real sender
		// does not pass (Go renders the missing map key as "<no value>").
		if strings.Contains(out, "<no value>") {
			t.Fatalf("%s: references a placeholder not supplied by its sender", c.name)
		}
		if !strings.Contains(out, "#2463eb") {
			t.Fatalf("%s: missing Signal Blue", c.name)
		}
		lower := strings.ToLower(out)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Fatalf("%s: forbidden design pattern %q", c.name, bad)
			}
		}
	}
}

func TestLoadTemplate_LegacyStandaloneStillRenders(t *testing.T) {
	// A template without a {{define "content"}} block must still render on its own
	// (so the migration to the shared layout can be incremental).
	out, err := loaderForTests().LoadTemplate("welcome_email.html", map[string]interface{}{
		"Email": "user@example.com",
	})
	if err != nil {
		t.Fatalf("legacy LoadTemplate: %v", err)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Fatal("expected legacy template to render its placeholder")
	}
}
