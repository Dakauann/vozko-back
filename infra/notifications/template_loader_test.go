package notification_service

import (
	"path/filepath"
	"strings"
	"testing"

	"vozko/brand"
)

const (
	frontendPrimary           = "#00c28e"
	frontendPrimaryForeground = "#0e1011"
	frontendPrimaryInk        = "#007a5c"
	frontendCanvas            = "#f6f8f9"
	frontendForeground        = "#14171a"
	frontendMutedForeground   = "#58626a"
	frontendBorder            = "#dce1e5"
)

func loaderForTests() *TemplateLoaderService {
	return NewTemplateLoaderService("templates")
}


type templateCase struct {
	name    string
	data    map[string]interface{}
	hasCTA  bool
	mustSee []string
}

func everyTemplateWithItsSenderData() []templateCase {
	dash := "https://app.example.com/dashboard"
	return []templateCase{
		{"monthly_invoice_issued.html", map[string]interface{}{"Amount": "R$ 497,90", "DueDate": "23/11/2026", "PayURL": "https://pay.example.com/i/abc", "DashboardURL": dash + "/invoices",
			"Lines": []invoiceLineForTests{{Label: "Plano Growth", Amount: "R$ 397,00"}, {Label: "2 x Número de WhatsApp", Amount: "R$ 100,90"}}}, true, []string{"R$ 497,90", "23/11/2026", "Plano Growth", "2 x Número de WhatsApp", "pay.example.com/i/abc"}},
		{"billing_cutoff_warning.html", map[string]interface{}{"Amount": "R$ 497,90", "DueDate": "23/11/2026", "CutoffDate": "27/11/2026", "PayURL": "https://pay.example.com/i/abc"}, true, []string{"27/11/2026", "R$ 497,90", "números de WhatsApp"}},
		{"billing_access_cut.html", map[string]interface{}{"Amount": "R$ 497,90", "DueDate": "23/11/2026", "PlansURL": dash + "/plans"}, true, []string{"R$ 497,90", "números de WhatsApp", "/plans"}},
		{"comment_analysis_cap_reached.html", map[string]interface{}{"DailyCap": 20000, "Date": "2026-09-02", "DashboardURL": dash}, true, []string{"20000", "2026-09-02"}},
		{"email_verification.html", map[string]interface{}{"Email": "u@x.com", "Token": "123456"}, false, []string{"u@x.com", "123456"}},
		{"issue_created_confirmation.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "WorkspaceName": "Loja da Ana", "IssueDescription": "Tentei conectar.\nDeu erro.", "DashboardURL": dash + "/issues/1"}, true, []string{"João", "Loja da Ana", "Tentei conectar.<br>Deu erro."}},
		{"issue_response_notification.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "WorkspaceName": "Loja da Ana", "StatusLabel": "Em Andamento", "StatusColor": "#4A90E2", "AuthorName": "Carlos", "AuthorInitial": "C", "ResponseBody": "Olá, já estamos verificando.", "ResponseDate": "29/06/2026 10:00", "DashboardURL": dash + "/issues/1"}, true, []string{"Carlos", "29/06/2026 10:00", "Em Andamento"}},
		{"issue_status_update.html", map[string]interface{}{"OwnerName": "João", "IssueTitle": "Não consigo conectar o número", "WorkspaceName": "Loja da Ana", "OldStatusLabel": "Aberto", "OldStatusColor": "#4A90E2", "NewStatusLabel": "Fechado", "NewStatusColor": "#28a745", "IsClosed": true, "DashboardURL": dash + "/issues/1"}, true, []string{"Chamado encerrado", "Aberto", "Fechado"}},
		{"login_locked.html", map[string]interface{}{"Email": "u@x.com", "ResetURL": dash + "/reset-password"}, true, []string{"u@x.com", "reset-password"}},
		{"login_successful.html", map[string]interface{}{"Email": "u@x.com", "LoginTime": "2026-06-28 14:30:00"}, false, []string{"2026-06-28 14:30:00"}},
		{"low_balance_warning.html", map[string]interface{}{"Balance": "R$ 12,42", "DashboardURL": dash}, true, []string{"R$ 12,42"}},
		{"meta_ads_funds_alert.html", map[string]interface{}{"Headline": "Os anúncios pararam: os fundos acabaram", "Subtitle": "Adicione fundos na Meta para voltar a veicular", "Message": "Os fundos da conta de anúncios Loja na Meta acabaram.", "Situation": "Sem fundos", "Tone": "danger", "AccountName": "Loja", "ActionURL": "https://business.facebook.com/billing_hub/payment_settings", "ActionLabel": "Adicionar fundos na Meta", "ManagerURL": dash + "/advertising?account=a-1"}, true, []string{"Adicionar fundos na Meta", "Sem fundos", "advertising?account=a-1"}},
		{"password_changed.html", map[string]interface{}{"Email": "u@x.com", "DashboardURL": dash}, true, []string{"u@x.com"}},
		{"password_reset.html", map[string]interface{}{"Email": "u@x.com", "Token": "123456", "ExpiresIn": "1 hora"}, false, []string{"123456", "1 hora"}},
		{"password_reset_locked.html", map[string]interface{}{"Email": "u@x.com", "DashboardURL": dash}, true, []string{"u@x.com"}},
		{"plan_expiry_reminder.html", map[string]interface{}{"PlanName": "Plano Growth", "ExpiryDate": "12/07/2026", "DashboardURL": dash}, true, []string{"Plano Growth", "12/07/2026"}},
		{"wallet_topup_confirmed.html", map[string]interface{}{"Amount": "R$ 250,00", "DashboardURL": dash}, true, []string{"R$ 250,00"}},
		{"welcome_email.html", map[string]interface{}{"Email": "u@x.com"}, true, []string{"u@x.com"}},
		{"whatsapp_account_update.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "DashboardURL": dash, "Headline": "Nome de exibição aprovado", "Subtitle": "Loja", "Message": "O nome foi aprovado.", "StatusLabel": "Aprovado", "Tone": "success", "Glyph": "check"}, true, []string{"Nome de exibição aprovado", "Aprovado", "11 90000-0000"}},
		{"whatsapp_campaign_hold.html", map[string]interface{}{"Headline": "Campanha pausada automaticamente", "CampaignName": "Promo outubro", "Reason": "a Meta limitou o número por spam (131048)", "HeldAt": "16/10 12:56", "Wave": "3", "Advice": "Revise o motivo e retome a campanha.", "DashboardURL": dash + "/whatsapp-campaigns/1"}, true, []string{"Promo outubro", "16/10 12:56", "Revise o motivo e retome a campanha."}},
		{"whatsapp_number_banned.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "DashboardURL": dash}, true, []string{"11 90000-0000"}},
		{"whatsapp_number_live.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "DashboardURL": dash}, true, []string{"11 90000-0000"}},
		{"whatsapp_number_suspended.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "Reason": "Complemento ou plano inativo", "DashboardURL": dash}, true, []string{"Complemento ou plano inativo"}},
		{"whatsapp_onboarding_failed.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "Reason": "Número já registrado", "DashboardURL": dash}, true, []string{"Número já registrado"}},
		{"whatsapp_quality_alert.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "DashboardURL": dash, "Quality": "Baixa"}, true, []string{"Baixa"}},
		{"whatsapp_scheduled_disable.html", map[string]interface{}{"PhoneNumber": "+55 11 90000-0000", "DashboardURL": dash, "DisableDate": "30/07/2026"}, true, []string{"30/07/2026"}},
		{"workspace_invite.html", map[string]interface{}{"Email": "u@x.com", "InviterEmail": "admin@x.com", "WorkspaceName": "Equipe Vendas", "Role": "member", "RoleName": "Membro", "Token": "abc", "AcceptURL": "https://app.example.com/invite?token=abc", "LoginURL": "https://app.example.com/login"}, true, []string{"admin@x.com", "Equipe Vendas", "Membro", "invite?token=abc", "https://app.example.com/login"}},
		{"notification.html", fullNotificationData(), true, []string{"Campanha concluída"}},
		{"notification_digest.html", digestData(3), true, []string{"Campanha A pausada", "Saldo baixo", "e mais 3 avisos"}},
	}
}

type invoiceLineForTests struct {
	Label  string
	Amount string
}

type digestLineForTests struct {
	Title         string
	Body          string
	CategoryLabel string
	Tone          string
	ActionURL     string
	SentAt        string
}

func digestData(more int) map[string]interface{} {
	return map[string]interface{}{
		"WorkspaceName": "Loja da Ana", "Date": "18/10/2026", "Total": 2 + more, "More": more,
		"InboxURL": "https://app.example.com/dashboard",
		"Items": []digestLineForTests{
			{Title: "Campanha A pausada", Body: "O modelo foi pausado pela Meta.", CategoryLabel: "Campanhas", Tone: "warning", ActionURL: "https://app.example.com/dashboard/whatsapp-campaigns/1", SentAt: "17/10 14:02"},
			{Title: "Saldo baixo", Body: "O saldo da carteira está em R$ 2,30.", CategoryLabel: "Financeiro", Tone: "warning", ActionURL: "https://app.example.com/dashboard/balance", SentAt: "17/10 18:40"},
		},
	}
}

func TestDigestEmailListsEachItemWithItsLinkAndOnlyCountsTheRestWhenThereIsMore(t *testing.T) {
	out, err := loaderForTests().LoadTemplate("notification_digest.html", digestData(0))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertFollowsFrontendDesign(t, "notification_digest.html", out)
	for _, want := range []string{"Loja da Ana", "18/10/2026", "https://app.example.com/dashboard/balance", "17/10 18:40", "Financeiro"} {
		if !strings.Contains(out, want) {
			t.Fatalf("digest lacks %q", want)
		}
	}
	if strings.Contains(out, "e mais") {
		t.Fatal("without leftovers the digest does not mention more items")
	}
}

func fullNotificationData() map[string]interface{} {
	return map[string]interface{}{
		"Headline": "Campanha concluída", "Message": "A campanha terminou.\nTaxa de leitura: 68%.",
		"CategoryLabel": "Campanhas", "Tone": "success", "ImageURL": "https://cdn.example.com/relatorio.png",
		"ActionURL": "https://app.example.com/dashboard/whatsapp-campaigns/1", "ActionLabel": "Ver relatório",
		"InboxURL": "https://app.example.com/dashboard/notifications", "WorkspaceName": "Loja da Ana", "SentAt": "16/10/2026 14:30",
	}
}

func assertFollowsFrontendDesign(t *testing.T, name, out string) {
	t.Helper()
	for _, want := range []string{
		"<!DOCTYPE html>", `lang="pt-BR"`, `role="presentation"`,
		frontendCanvas, frontendForeground, frontendMutedForeground, frontendBorder, frontendPrimaryInk,
		`name="color-scheme" content="light dark"`, "prefers-color-scheme: dark", "[data-ogsc]", "[data-ogsb]",
		"max-width: 620px", "Oxanium", "Arial,sans-serif",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s: expected %q in the rendered email", name, want)
		}
	}
	lower := strings.ToLower(out)
	forbidden := map[string]string{
		"linear-gradient":  "gradients are not allowed in transactional UI",
		"#2463eb":          "the retired Signal Blue primary must be gone; the frontend primary is green",
		"#4a90e2":          "the old non-brand blue must be gone",
		"#1a1a1a":          "near-black text must be the frontend foreground",
		"border-left:4px":  "thick colored left-border accent stripes are forbidden",
		"border-left: 4px": "thick colored left-border accent stripes are forbidden",
		"<svg":             "email clients strip inline SVG",
		"background-image": "meaning must never ride on a background image",
		"<no value>":       "a placeholder the sender does not supply",
	}
	for pat, why := range forbidden {
		if strings.Contains(lower, pat) {
			t.Fatalf("%s: found forbidden pattern %q: %s", name, pat, why)
		}
	}
	if strings.ContainsRune(out, '—') || strings.ContainsRune(out, '–') {
		t.Fatalf("%s: em-dash/en-dash found in email copy", name)
	}
}

func TestLoadTemplate_ComposesLayoutAndRendersData(t *testing.T) {
	brand.SetForTest(brand.Brand{
		Name: "Vozko", LegalName: "Vozko", CNPJ: "63.819.955/0001-95",
		SiteURL: "https://vozkoia.com", EmailDomain: "vozkoia.com",
		SupportEmail: "suporte@vozkoia.com", LogoURL: "https://cdn.vozkoia.com/logo.png",
		Gender: brand.Feminine,
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

	for _, want := range []string{"<!DOCTYPE html>", "Vozko", "CNPJ 63.819.955/0001-95", "vozkoia.com", "suporte@vozkoia.com", "https://cdn.vozkoia.com/logo.png"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered email to contain %q", want)
		}
	}
	for _, want := range []string{"user@example.com", "482913", "15 minutos"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered email to contain placeholder value %q", want)
		}
	}
}

func TestEveryTemplateFileHasItsSenderCase(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range everyTemplateWithItsSenderData() {
		covered[c.name] = true
	}
	files, err := filepath.Glob(filepath.Join("templates", "*.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !covered[name] {
			t.Errorf("%s has no sender case: either a sender uses it and the case is missing, or nothing sends it and it should be deleted", name)
		}
	}
}

func TestEveryTemplate_RendersItsSenderDataInTheFrontendDesign(t *testing.T) {
	loader := loaderForTests()
	for _, c := range everyTemplateWithItsSenderData() {
		out, err := loader.LoadTemplate(c.name, c.data)
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		assertFollowsFrontendDesign(t, c.name, out)
		for _, want := range c.mustSee {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: expected sender value %q in the email", c.name, want)
			}
		}
	}
}

func TestEveryCallToAction_IsTheFrontendPrimaryButtonWithAnOutlookFallback(t *testing.T) {
	loader := loaderForTests()
	for _, c := range everyTemplateWithItsSenderData() {
		if !c.hasCTA {
			continue
		}
		out, err := loader.LoadTemplate(c.name, c.data)
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		for _, want := range []string{frontendPrimary, frontendPrimaryForeground, "<v:roundrect", "<!--[if mso]>", "<!--[if !mso]><!-->", "border-radius:6px"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: button is missing %q", c.name, want)
			}
		}
	}
}

func TestEveryTemplate_TakesTheBrandOnlyFromConfiguration(t *testing.T) {
	brand.SetForTest(brand.Brand{
		Name: "Acme", LegalName: "Acme Comercio LTDA", CNPJ: "11.222.333/0001-44",
		SiteURL: "https://acme.example", EmailDomain: "acme.example",
		SupportEmail: "ajuda@acme.example", LogoURL: "https://cdn.acme.example/logo.png",
		Gender: brand.Feminine,
	})
	t.Cleanup(func() { brand.SetForTest(brand.Brand{}) })

	loader := loaderForTests()
	for _, c := range everyTemplateWithItsSenderData() {
		out, err := loader.LoadTemplate(c.name, c.data)
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		lower := strings.ToLower(out)
		for _, typed := range []string{"vozko", "global", "63.819.955"} {
			if strings.Contains(lower, typed) {
				t.Fatalf("%s: brand text %q is typed into the template instead of read from the brand configuration", c.name, typed)
			}
		}
		for _, want := range []string{"Acme", "Acme Comercio LTDA", "11.222.333/0001-44", "ajuda@acme.example", "https://cdn.acme.example/logo.png"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: expected brand value %q", c.name, want)
			}
		}
	}
}

func TestNotificationEmail_WithEveryOptionalField(t *testing.T) {
	out, err := loaderForTests().LoadTemplate("notification.html", fullNotificationData())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertFollowsFrontendDesign(t, "notification.html", out)
	for _, want := range []string{
		"<title>Campanha concluída - ",
		"A campanha terminou.<br>Taxa de leitura: 68%.",
		"Campanhas", "vx-tone-success",
		`<img src="https://cdn.example.com/relatorio.png" alt="Campanha concluída"`,
		`href="https://app.example.com/dashboard/whatsapp-campaigns/1"`, "Ver relatório",
		`href="https://app.example.com/dashboard/notifications"`, "Ver todas as notificações",
		"Loja da Ana", "16/10/2026 14:30",
		frontendPrimary,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in the notification email", want)
		}
	}
}

func TestNotificationEmail_WithoutOptionalFields(t *testing.T) {
	out, err := loaderForTests().LoadTemplate("notification.html", map[string]interface{}{
		"Headline": "Manutenção programada", "Message": "O sistema fica indisponível às 23h. <b>Salve</b> o trabalho.",
		"CategoryLabel": "Comunicados", "Tone": "warning", "ImageURL": "", "ActionURL": "", "ActionLabel": "",
		"InboxURL": "https://app.example.com/dashboard/notifications", "WorkspaceName": "", "SentAt": "16/10/2026 14:30",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	assertFollowsFrontendDesign(t, "notification.html", out)
	if got := strings.Count(out, "<img "); got != 1 {
		t.Fatalf("expected only the logo image without ImageURL, got %d images", got)
	}
	if strings.Contains(out, "Área de trabalho") {
		t.Fatal("workspace line must be omitted without WorkspaceName")
	}
	if !strings.Contains(out, "&lt;b&gt;Salve&lt;/b&gt;") {
		t.Fatal("the plain-text message must be escaped, never rendered as HTML")
	}
	for _, want := range []string{"Comunicados", "vx-tone-warning", "Ver todas as notificações", `href="https://app.example.com/dashboard/notifications"`, "<v:roundrect", "16/10/2026 14:30"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in the minimal notification email", want)
		}
	}
}

func TestTextLines_SplitsOnEveryLineEnding(t *testing.T) {
	got := textLines("  um\r\ndois\n\ntrês  ")
	want := []string{"um", "dois", "", "três"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("textLines = %q, want %q", got, want)
	}
}

func TestButtonWidth_FitsTheLabelWithAFloor(t *testing.T) {
	if got := buttonWidth("Ir"); got != buttonMinWidth {
		t.Fatalf("short label width = %d, want the floor %d", got, buttonMinWidth)
	}
	long := "Adicionar fundos na Meta"
	if got := buttonWidth(long); got <= buttonMinWidth || got != len([]rune(long))*buttonCharWidth+buttonSidePadding {
		t.Fatalf("long label width = %d", got)
	}
}
