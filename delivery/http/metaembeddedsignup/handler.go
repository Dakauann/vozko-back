package metaembeddedsignup

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"vozko/brand"
	"vozko/delivery/http/response"
	"vozko/domain/coexistence"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	whatsappcampaign "vozko/domain/whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type MetaEmbeddedSignupHandler struct {
	appID       string
	configID    string
	graphAPIVer string

	solutionID    string
	esFeatureType string
	esVersion     string

	dialog360              businessphone.Dialog360Onboarder
	dialog360WebhookSecret string

	templateWebhook template.HandleTemplateWebhookUseCase

	// Native Meta (Cloud API) onboarding path. Used when dialog360OnboardingEnabled
	// is false: the callback exchanges the Embedded Signup code for a Graph token,
	// registers the number directly on Meta and persists a ProviderMeta phone.
	// Wired via WithMeta; the 360dialog fields above are left untouched.
	dialog360OnboardingEnabled   bool
	appSecret                    string
	httpClient                   *http.Client
	onboardUseCase               businessphone.OnboardEmbeddedSignupUseCase
	coexistenceService           coexistence.MetaCoexistenceService
	ensureOrganicCampaignUseCase whatsappcampaign.EnsureOrganicCoexistenceCampaignUseCase
}

func (h *MetaEmbeddedSignupHandler) WithDialog360(onboarder businessphone.Dialog360Onboarder, webhookSecret string) *MetaEmbeddedSignupHandler {
	h.dialog360 = onboarder
	h.dialog360WebhookSecret = webhookSecret
	return h
}

func (h *MetaEmbeddedSignupHandler) WithTemplateWebhook(uc template.HandleTemplateWebhookUseCase) *MetaEmbeddedSignupHandler {
	h.templateWebhook = uc
	return h
}

type MetaEmbeddedSignupConfig struct {
	AppID         string
	ConfigID      string
	SolutionID    string
	ESFeatureType string
	ESVersion     string
}

func NewMetaEmbeddedSignupHandler(cfg MetaEmbeddedSignupConfig) *MetaEmbeddedSignupHandler {
	esVersion := strings.TrimSpace(cfg.ESVersion)
	if esVersion == "" {
		esVersion = "v4"
	}
	return &MetaEmbeddedSignupHandler{
		appID:         cfg.AppID,
		configID:      cfg.ConfigID,
		graphAPIVer:   "v22.0",
		solutionID:    strings.TrimSpace(cfg.SolutionID),
		esFeatureType: strings.TrimSpace(cfg.ESFeatureType),
		esVersion:     esVersion,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

// WithMeta wires the native Meta (Cloud API) onboarding path and the toggle that
// selects it. onboardingVia360dialog mirrors ENABLE_360DIALOG_ONBOARDING: when
// false (the default) the callback runs the native Meta flow (code -> Graph token
// -> register -> persist a ProviderMeta phone); when true it defers to the
// existing 360dialog handover wired by WithDialog360, which stays untouched.
func (h *MetaEmbeddedSignupHandler) WithMeta(
	appSecret string,
	onboardUC businessphone.OnboardEmbeddedSignupUseCase,
	coexSvc coexistence.MetaCoexistenceService,
	ensureOrganicCampaignUC whatsappcampaign.EnsureOrganicCoexistenceCampaignUseCase,
	onboardingVia360dialog bool,
) *MetaEmbeddedSignupHandler {
	h.appSecret = strings.TrimSpace(appSecret)
	h.onboardUseCase = onboardUC
	h.coexistenceService = coexSvc
	h.ensureOrganicCampaignUseCase = ensureOrganicCampaignUC
	h.dialog360OnboardingEnabled = onboardingVia360dialog
	if h.httpClient == nil {
		h.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return h
}

func (h *MetaEmbeddedSignupHandler) embeddedSignupExtras() string {
	setup := "{}"
	// solutionID is a 360dialog Multi-Partner-Solution concept. It must only be
	// injected on the 360dialog onboarding path; on the native Meta (Tech Provider)
	// path it corrupts the flow, so setup stays empty there.
	if h.dialog360OnboardingEnabled && h.solutionID != "" && h.esFeatureType == "" {
		setup = fmt.Sprintf("{ solutionID: '%s' }", h.solutionID)
	}
	flow := fmt.Sprintf("version: '%s'", h.esVersion)
	if h.esFeatureType != "" {
		flow = fmt.Sprintf("featureType: '%s', version: '%s'", h.esFeatureType, h.esVersion)
	}
	return fmt.Sprintf("{ setup: %s, sessionInfoVersion: '3', %s }", setup, flow)
}

// OnboardingConfigResponse tells the dashboard which onboarding path the server
// is running and whether that path consumes a workspace phone-capacity slot. The
// capacity gate reads it so it never blocks a slot-free (native Meta) onboarding,
// and always gates the slot-billed 360dialog one, keeping the UI in lockstep with
// ENABLE_360DIALOG_ONBOARDING instead of hardcoding an assumption.
type OnboardingConfigResponse struct {
	Provider     string `json:"provider"`     // "meta" | "dialog360"
	RequiresSlot bool   `json:"requiresSlot"` // true only for the 360dialog path
}

// @Summary		Configuração de onboarding do WhatsApp
// @Description	Informa qual provedor de onboarding está ativo e se ele consome um slot de capacidade do workspace.
// @Tags			Cadastro incorporado do WhatsApp
// @Produce		json
// @Success		200	{object}	OnboardingConfigResponse
// @Security		BearerAuth
// @Router			/whatsapp/onboarding-config [get]
func (h *MetaEmbeddedSignupHandler) GetOnboardingConfig(w http.ResponseWriter, r *http.Request) {
	provider := "meta"
	if h.dialog360OnboardingEnabled {
		provider = "dialog360"
	}
	response.WriteSuccess(w, http.StatusOK, OnboardingConfigResponse{
		Provider:     provider,
		RequiresSlot: h.dialog360OnboardingEnabled,
	})
}

func (h *MetaEmbeddedSignupHandler) requireOwnerContext(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	ownerWorkspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if ownerWorkspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace_id is required", map[string]string{
			"workspace_id": "string (required query parameter for explicit ownership assignment)",
		})
		return "", "", false
	}

	claims := middleware.GetClaims(r)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return "", "", false
	}

	return ownerWorkspaceID, claims.UserID, true
}

// @Summary		Página de cadastro incorporado do WhatsApp
// @Description	Renderiza a página de cadastro incorporado (Embedded Signup) da Meta que conecta um número do WhatsApp Business ao workspace via 360dialog.
// @Tags			Cadastro incorporado do WhatsApp
// @Produce		html
// @Param			workspace_id		query		string	true	"Identificador do workspace proprietário do número"
// @Param			redirect_url		query		string	false	"URL de retorno após a conclusão do cadastro"
// @Param			error				query		string	false	"Código de erro retornado pela Meta"
// @Param			error_description	query		string	false	"Descrição do erro retornado pela Meta"
// @Success		200	{string}	string	"Página HTML de cadastro"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/meta/embedded [get]
func (h *MetaEmbeddedSignupHandler) ServeEmbeddedSignupPage(w http.ResponseWriter, r *http.Request) {
	log.Printf("[meta-embedded-signup] Page request from %s", r.RemoteAddr)

	ownerWorkspaceID, _, ok := h.requireOwnerContext(w, r)
	if !ok {
		return
	}

	redirectAfterSuccess := r.URL.Query().Get("redirect_url")

	errorCode := r.URL.Query().Get("error")
	if errorCode != "" {
		errorDesc := r.URL.Query().Get("error_description")
		log.Printf("[meta-embedded-signup] Error from Meta redirect: %s - %s", errorCode, errorDesc)
	}

	configID := h.configID
	if configID == "" {
		configID = "872229022465818"
	}

	safeRedirectURL := strings.ReplaceAll(redirectAfterSuccess, "'", "\\'")
	safeRedirectURL = strings.ReplaceAll(safeRedirectURL, "\\", "\\\\")

	logoMarkup := fmt.Sprintf(`<img class="logo-img" src="%s" alt="%s">`,
		html.EscapeString(brand.Active().LogoURL), html.EscapeString(brand.Active().Name))
	brandNameHTML := html.EscapeString(brand.Active().Name)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - Conectar WhatsApp Business</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:wght@300;400;500&family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        *, *::before, *::after { margin: 0; padding: 0; box-sizing: border-box; }

        :root {
            /* Signal Blue, one action voice */
            --primary: #2463eb;
            --primary-hover: #1d4fd7;
            --primary-active: #1e3fae;
            --primary-50: #eef3ff;
            --primary-100: #dce7ff;
            --foreground: #344256;          /* Ink, never pure black */
            --slate-50: #f5f5f5;            /* Canvas */
            --slate-100: #f1f5f9;           /* Mist */
            --slate-200: #e1e7ef;           /* Hairline */
            --slate-300: #cbd5e1;
            --slate-400: #94a3b8;
            --slate-500: #65758b;           /* Slate Mute */
            --slate-700: #344256;
            --slate-900: #1d2433;
            --success-tint: #eef7f3;
            --success-ink: #2f604b;
            --danger: #ef4444;
            --whatsapp: #25D366;
            --whatsapp-dark: #128C7E;
            --radius: 12px;
            /* Soft, slate-tinted layered elevation + inner highlight */
            --shadow-soft: 0 18px 32px -18px rgba(15,23,42,0.18), 0 8px 20px -12px rgba(15,23,42,0.14);
            --shadow-inset: inset 0 1px 0 rgba(255,255,255,0.14);
            --shadow-hover: 0 24px 40px -18px rgba(15,23,42,0.2), 0 10px 24px -12px rgba(15,23,42,0.16);
        }

        body {
            font-family: 'Inter', system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
            color: var(--foreground);
            background: var(--slate-50);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            padding: 24px;
            position: relative;
            overflow-x: hidden;
        }

        /* Faint engineered grid + a single soft blue wash, Quiet Infrastructure */
        body::before {
            content: '';
            position: fixed;
            inset: 0;
            z-index: 0;
            pointer-events: none;
            background-image:
                radial-gradient(110%% 70%% at 50%% -8%%, rgba(36,99,235,0.07), transparent 62%%),
                linear-gradient(var(--slate-200) 1px, transparent 1px),
                linear-gradient(90deg, var(--slate-200) 1px, transparent 1px);
            background-size: 100%% 100%%, 38px 38px, 38px 38px;
            opacity: 0.55;
            -webkit-mask-image: radial-gradient(78%% 56%% at 50%% 42%%, #000 28%%, transparent 76%%);
            mask-image: radial-gradient(78%% 56%% at 50%% 42%%, #000 28%%, transparent 76%%);
        }

        .page-container {
            position: relative;
            z-index: 1;
            width: 100%%;
            max-width: 480px;
            animation: fadeSlideUp 0.5s ease-out;
        }

        @keyframes fadeSlideUp {
            from { opacity: 0; transform: translateY(16px); }
            to   { opacity: 1; transform: translateY(0); }
        }

        .card {
            background: #ffffff;
            border: 1px solid var(--slate-200);
            border-radius: 16px;
            padding: 34px 32px;
            box-shadow: var(--shadow-soft), var(--shadow-inset);
        }

        /* Brand logo (single CDN asset) */
        .logo-container {
            display: flex;
            align-items: center;
            margin-bottom: 26px;
        }
        .logo-img { height: 34px; width: auto; display: block; }
        /* Fallback wordmark when the frontend logo origin is unavailable */

        .card-header {
            margin-bottom: 22px;
        }

        h1 {
            font-size: 23px;
            font-weight: 700;
            color: var(--foreground);
            letter-spacing: -0.02em;
            line-height: 1.2;
            margin-bottom: 8px;
            text-wrap: balance;
        }

        .subtitle {
            font-size: 14px;
            color: var(--slate-500);
            line-height: 1.6;
            max-width: 38ch;
        }

        .steps {
            display: flex;
            flex-direction: column;
            margin: 0 0 24px;
        }
        .step {
            display: flex;
            align-items: center;
            gap: 12px;
            padding: 11px 2px;
            font-size: 13.5px;
            color: var(--foreground);
        }
        .step + .step { border-top: 1px solid var(--slate-200); }
        .step-number {
            width: 24px;
            height: 24px;
            border-radius: 7px;
            background: var(--primary);
            box-shadow: var(--shadow-inset);
            color: #fff;
            font-size: 12px;
            font-weight: 600;
            display: flex;
            align-items: center;
            justify-content: center;
            flex-shrink: 0;
        }

        .btn-primary {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 9px;
            width: 100%%;
            height: 46px;
            padding: 0 22px;
            border: none;
            border-radius: 12px;
            background: var(--primary);
            color: #fff;
            font-family: inherit;
            font-size: 14.5px;
            font-weight: 600;
            letter-spacing: -0.01em;
            cursor: pointer;
            box-shadow: var(--shadow-soft), var(--shadow-inset);
            transition: background 0.18s ease, box-shadow 0.18s ease;
        }
        .btn-primary:hover { background: var(--primary-hover); box-shadow: var(--shadow-hover), var(--shadow-inset); }
        .btn-primary:active { background: var(--primary-active); }
        .btn-primary:disabled { background: #aebfd9; cursor: default; box-shadow: none; }
        .btn-primary:focus-visible { outline: none; box-shadow: 0 0 0 2px #fff, 0 0 0 4px rgba(36,99,235,0.45); }
        .btn-primary svg { width: 18px; height: 18px; }

        .divider {
            display: flex;
            align-items: center;
            margin: 24px 0;
            gap: 12px;
        }
        .divider::before, .divider::after {
            content: '';
            flex: 1;
            height: 1px;
            background: var(--slate-200);
        }
        .divider span {
            font-size: 12px;
            color: var(--slate-400);
            font-weight: 500;
        }

        /* Status banners */
        .status {
            margin-top: 18px;
            padding: 12px 14px;
            border-radius: 12px;
            font-size: 13px;
            line-height: 1.5;
            display: none;
            text-align: left;
            word-break: break-word;
            border: 1px solid transparent;
        }
        .status.info    { display: block; background: var(--slate-100); border-color: var(--slate-200); color: var(--foreground); }
        .status.success { display: block; background: var(--success-tint); border-color: #cfe6db; color: var(--success-ink); }
        .status.error   { display: block; background: #fef2f2; border-color: #fbd5d5; color: #b42318; }

        .status-icon { font-weight: 600; margin-right: 5px; }

        /* Redirect / finish notice */
        .redirect-notice {
            margin-top: 12px;
            font-size: 12.5px;
            color: var(--slate-500);
            text-align: center;
            display: none;
        }
        .redirect-notice.visible { display: block; }
        .redirect-notice strong { color: var(--foreground); }
        .redirect-notice a { color: var(--primary); text-decoration: none; font-weight: 600; }

        .footer-text {
            text-align: center;
            margin-top: 18px;
            font-size: 11.5px;
            color: var(--slate-500);
        }
        .footer-text a {
            color: var(--slate-500);
            text-decoration: none;
        }
        .footer-text a:hover {
            color: var(--primary);
        }

        /* Spinner */
        .spinner { display: inline-block; width: 17px; height: 17px; border: 2px solid rgba(255,255,255,0.35); border-top-color: #fff; border-radius: 50%%; animation: spin 0.6s linear infinite; }
        @keyframes spin { to { transform: rotate(360deg); } }

        @media (max-width: 480px) {
            .card { padding: 28px 22px; }
            h1 { font-size: 21px; }
        }
        @media (prefers-reduced-motion: reduce) {
            .page-container { animation: none; }
            .spinner { animation-duration: 1.2s; }
            .btn-primary { transition: none; }
        }
    </style>
</head>
<body>
    <div class="page-container">
        <div class="card">
            <div class="logo-container">%s</div>

            <div class="card-header">
                <h1>Conectar WhatsApp Business</h1>
                <p class="subtitle">Vincule seu número à %s com o cadastro oficial da Meta. Leva menos de dois minutos.</p>
            </div>

            <div class="steps">
                <div class="step"><span class="step-number">1</span> Entre com sua conta da Meta</div>
                <div class="step"><span class="step-number">2</span> Selecione ou crie sua conta empresarial</div>
                <div class="step"><span class="step-number">3</span> Registre seu número do WhatsApp</div>
            </div>

            <button class="btn-primary" id="signupBtn" onclick="launchWhatsAppSignup()">
                <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m13 6 6 6-6 6"/></svg>
                Iniciar cadastro
            </button>

            <div class="status" id="status"></div>
            <div class="redirect-notice" id="redirectNotice"></div>
        </div>

        <p class="footer-text">
            Cadastro oficial via Meta &middot; protegido por OAuth 2.0
        </p>
    </div>

    <!-- FB SDK init MUST be defined before loading the SDK script -->
    <script>
        window.fbAsyncInit = function() {
            FB.init({
                appId: '%s',
                autoLogAppEvents: true,
                xfbml: true,
                version: '%s'
            });
            window._fbSDKReady = true;
        };
    </script>
    <script async defer crossorigin="anonymous" src="https://connect.facebook.net/pt_BR/sdk.js"></script>

    <script>
        const statusEl = document.getElementById('status');
        const redirectNoticeEl = document.getElementById('redirectNotice');
        const REDIRECT_URL = '%s';
		const OWNER_WORKSPACE_ID = '%s';

        function showStatus(type, message) {
            statusEl.className = 'status ' + type;
            statusEl.innerHTML = message;
        }

        function startRedirectCountdown(params) {
            if (!REDIRECT_URL) return;
            let seconds = 5;
            const sep = REDIRECT_URL.includes('?') ? '&' : '?';
            const targetURL = REDIRECT_URL + sep + new URLSearchParams(params).toString();
            redirectNoticeEl.classList.add('visible');
            redirectNoticeEl.innerHTML = 'Redirecionando em <strong>' + seconds + 's</strong>… <a href="' + targetURL + '" style="color:inherit;text-decoration:underline;">Ir agora</a>';
            const interval = setInterval(() => {
                seconds--;
                if (seconds <= 0) {
                    clearInterval(interval);
                    window.location.href = targetURL;
                } else {
                    redirectNoticeEl.innerHTML = 'Redirecionando em <strong>' + seconds + 's</strong>… <a href="' + targetURL + '" style="color:inherit;text-decoration:underline;">Ir agora</a>';
                }
            }, 1000);
        }

        function frontendOrigin() {
            try { return new URL(REDIRECT_URL).origin; } catch (e) { return '*'; }
        }

        // When opened as a popup from the dashboard, hand the result back to the
        // opener and close instead of navigating; otherwise fall back to the
        // full-page redirect so the page still works when opened directly.
        function finishFlow(params) {
            var opener = window.opener;
            if (opener && !opener.closed) {
                try {
                    opener.postMessage(Object.assign({ source: 'wa-embedded' }, params), frontendOrigin());
                } catch (e) {}
                redirectNoticeEl.classList.add('visible');
                redirectNoticeEl.innerHTML = 'Concluído. Você pode fechar esta janela.';
                setTimeout(function () { try { window.close(); } catch (e) {} }, 1200);
                return;
            }
            startRedirectCountdown(params);
        }

        window.addEventListener('message', (event) => {
            if (!event.origin.endsWith('facebook.com')) return;
            try {
                const data = JSON.parse(event.data);
                if (data.type === 'WA_EMBEDDED_SIGNUP') {
                    if (data.event === 'FINISH' || data.event === 'FINISH_ONLY_WABA' || data.event === 'FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING') {
                        window._embeddedSignupData = data.data;
                    } else if (data.event === 'CANCEL') {
                        showStatus('error', '<span class="status-icon">✕</span> Cadastro cancelado na etapa: ' + (data.data.current_step || 'desconhecida'));
                    }
                }
            } catch(e) {}
        });

        function waitForEmbeddedData(maxWaitMs, intervalMs) {
            return new Promise((resolve) => {
                if (window._embeddedSignupData) { resolve(window._embeddedSignupData); return; }
                let elapsed = 0;
                const timer = setInterval(() => {
                    elapsed += intervalMs;
                    if (window._embeddedSignupData) { clearInterval(timer); resolve(window._embeddedSignupData); return; }
                    if (elapsed >= maxWaitMs) { clearInterval(timer); resolve(null); }
                }, intervalMs);
            });
        }

        const fbLoginCallback = (response) => {
            if (response.authResponse) {
                const code = response.authResponse.code;
                const accessToken = response.authResponse.accessToken;

                showStatus('info', '<span class="status-icon">⏳</span> Processando… aguardando dados da Meta…');
                document.getElementById('signupBtn').disabled = true;
                document.getElementById('signupBtn').innerHTML = '<span class="spinner"></span> Processando…';

                waitForEmbeddedData(5000, 200).then((embeddedData) => {
                    if (!embeddedData) {
                        console.warn('[embedded-signup] WA_EMBEDDED_SIGNUP postMessage not received within 5s, proceeding with server-side fallback');
                    } else {
                        console.log('[embedded-signup] Got embedded data:', embeddedData);
                    }

                    const payload = {
                        code: code || '',
                        access_token: accessToken || '',
                        phone_number_id: embeddedData ? embeddedData.phone_number_id : '',
                        waba_id: embeddedData ? embeddedData.waba_id : '',
                        business_id: embeddedData ? embeddedData.business_id : '',
                        user_id: response.authResponse.userID || '',
                        expires_in: response.authResponse.expiresIn || 0,
                        signed_request: response.authResponse.signedRequest || '',
                        full_response: response,
                        embedded_data: embeddedData || null
                    };

                    showStatus('info', '<span class="status-icon">⏳</span> Processando… trocando credenciais com a Meta.');

					fetch('/oauth/meta/embedded?workspace_id=' + encodeURIComponent(OWNER_WORKSPACE_ID), {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                })
                .then(res => res.json())
                .then(data => {
                    const btn = document.getElementById('signupBtn');
                    if (data.data && data.data.status === 'success') {
                        const phoneId = (data.data.db_save && data.data.db_save.phone_id) || data.data.phone_number_id || '';
                        const wabaId = data.data.waba_id || '';
                        showStatus('success', '<span class="status-icon">✓</span> <strong>WhatsApp Business conectado com sucesso!</strong><br><br>ID do Telefone: ' + (data.data.phone_number_id || 'N/A') + '<br>ID da WABA: ' + wabaId + '<br>ID da Empresa: ' + (data.data.business_id || 'N/A'));
                        btn.innerHTML = '✓ Conectado';
                        btn.style.background = '#059669';
                        finishFlow({ status: 'success', phone_id: phoneId, waba_id: wabaId });
                    } else if (data.data && data.data.status === 'code_received_but_no_app_secret') {
                        showStatus('info', '<span class="status-icon">⚠</span> <strong>Código recebido</strong>, META_APP_SECRET não configurado no servidor. Contate o administrador.');
                        btn.disabled = false;
                        btn.innerHTML = 'Iniciar Cadastro WhatsApp';
                    } else if (data.error) {
                        showStatus('error', '<span class="status-icon">✕</span> ' + (data.message || 'Erro desconhecido'));
                        btn.disabled = false;
                        btn.innerHTML = 'Iniciar Cadastro WhatsApp';
                    } else {
                        showStatus('success', '<span class="status-icon">✓</span> <strong>Concluído!</strong> Resposta recebida do servidor.');
                        btn.innerHTML = '✓ Concluído';
                        btn.style.background = '#059669';
                        finishFlow({ status: 'success' });
                    }
                })
                .catch(err => {
                    showStatus('error', '<span class="status-icon">✕</span> Erro de comunicação: ' + err.message);
                    const btn = document.getElementById('signupBtn');
                    btn.disabled = false;
                    btn.innerHTML = 'Iniciar Cadastro WhatsApp';
                });
                }); // end waitForEmbeddedData
            } else {
                showStatus('error', '<span class="status-icon">✕</span> Login cancelado ou falhou. Por favor, tente novamente.');
            }
        };

        const launchWhatsAppSignup = () => {
            if (typeof FB === 'undefined' || !window._fbSDKReady) {
                showStatus('error', '<span class="status-icon">✕</span> SDK do Facebook ainda está carregando. Aguarde um momento e tente novamente.');
                return;
            }
            showStatus('info', '<span class="status-icon">⏳</span> Abrindo janela de cadastro da Meta…');

            FB.login(fbLoginCallback, {
                config_id: '%s',
                response_type: 'code',
                override_default_response_type: true,
                extras: %s
            });
        };
    </script>
</body>
</html>`,
		brandNameHTML,
		logoMarkup,
		brandNameHTML,
		h.appID,
		h.graphAPIVer,
		safeRedirectURL,
		ownerWorkspaceID,
		configID,
		h.embeddedSignupExtras(),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

// @Summary		Concluir cadastro incorporado do WhatsApp
// @Description	Recebe o resultado do cadastro incorporado (Embedded Signup) e realiza o handover do número para o 360dialog (account_sharing).
// @Tags			Cadastro incorporado do WhatsApp
// @Accept			json
// @Produce		json
// @Param			workspace_id	query		string							true	"Identificador do workspace proprietário do número"
// @Param			body			body		EmbeddedSignupCallbackRequest	true	"Credenciais e dados do número retornados pela Meta"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		502	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/oauth/meta/embedded [post]
func (h *MetaEmbeddedSignupHandler) HandleEmbeddedSignupCallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("[meta-embedded-signup] Callback from %s", r.RemoteAddr)

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Failed to read body: %v", err)
		response.WriteError(w, http.StatusBadRequest, "failed to read request body", nil)
		return
	}
	defer r.Body.Close()

	var req EmbeddedSignupCallbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[meta-embedded-signup] ❌ Failed to parse JSON body: %v", err)
		response.WriteError(w, http.StatusBadRequest, "invalid JSON body", nil)
		return
	}

	log.Printf("[meta-embedded-signup] Callback data: phone=%s waba=%s business=%s hasCode=%v hasToken=%v",
		req.PhoneNumberID, req.WABAID, req.BusinessID, req.Code != "", req.AccessToken != "")

	ownerWorkspaceID, ownerAssignedBy, ok := h.requireOwnerContext(w, r)
	if !ok {
		return
	}

	// Native Meta (Cloud API) onboarding is the default path. Only when
	// ENABLE_360DIALOG_ONBOARDING is set do we hand the number to 360dialog below.
	if !h.dialog360OnboardingEnabled {
		h.handleMetaOnboarding(w, r, req, ownerWorkspaceID, ownerAssignedBy)
		return
	}

	if h.dialog360 == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "360dialog onboarding is not configured", nil)
		return
	}

	clientEmail := ""
	if claims := middleware.GetClaims(r); claims != nil {
		clientEmail = strings.TrimSpace(claims.Email)
	}
	if clientEmail == "" {
		clientEmail = "workspace-" + ownerWorkspaceID + "@" + brand.Active().EmailDomain
	}
	h.handleDialog360Provision(w, req, ownerWorkspaceID, ownerAssignedBy, clientEmail)
}

func GetMetaEmbeddedSignupConfigFromEnv() MetaEmbeddedSignupConfig {
	return MetaEmbeddedSignupConfig{
		AppID:         strings.TrimSpace(os.Getenv("WHATSAPP_APP_ID")),
		ConfigID:      strings.TrimSpace(os.Getenv("META_CONFIG_ID")),
		SolutionID:    strings.TrimSpace(os.Getenv("D360_SOLUTION_ID")),
		ESFeatureType: strings.TrimSpace(os.Getenv("WHATSAPP_ES_FEATURE_TYPE")),
		ESVersion:     strings.TrimSpace(os.Getenv("WHATSAPP_ES_VERSION")),
	}
}

func (h *MetaEmbeddedSignupHandler) handleDialog360Provision(w http.ResponseWriter, req EmbeddedSignupCallbackRequest, workspaceID, userID, clientEmail string) {
	phone, err := h.dialog360.StartProvisioning(businessphone.Dialog360ProvisionInput{
		WABAExternalID:   req.WABAID,
		PhoneNumberID:    req.PhoneNumberID,
		OwnerWorkspaceID: workspaceID,
		OwnerAssignedBy:  userID,
		ClientName:       clientEmail,
		ClientEmail:      clientEmail,
	})
	if err != nil {
		if errors.Is(err, businessphone.ErrPhoneLimitReached) {
			response.WriteErrorWithCode(w, http.StatusForbidden, "phone_limit_reached",
				"WhatsApp number capacity reached for this workspace.", nil)
			return
		}
		details := map[string]string{}
		if phone != nil {
			details["phone_id"] = phone.ID
			details["status"] = string(phone.Status)
		}
		log.Printf("[dialog360-onboarding] handover failed (phone visible as failed): %v", err)
		response.WriteError(w, http.StatusBadGateway, "360dialog onboarding failed: "+err.Error(), details)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"status":          "success",
		"provider":        "dialog360",
		"provisioning":    true,
		"phone_id":        phone.ID,
		"phone_number_id": phone.MetaPhoneNumberID,
		"waba_id":         phone.WABAId,
	})
}

// handleMetaOnboarding runs the native Meta (Cloud API) onboarding for a number
// that completed Embedded Signup. It exchanges the code for a Graph token (or uses
// a directly supplied access_token), then registers and persists the number.
func (h *MetaEmbeddedSignupHandler) handleMetaOnboarding(w http.ResponseWriter, r *http.Request, req EmbeddedSignupCallbackRequest, ownerWorkspaceID, ownerAssignedBy string) {
	_ = r
	code := req.Code
	if code == "" && req.AccessToken != "" {
		log.Printf("[meta-embedded-signup] Using direct access_token (no code)")
		h.processWithToken(w, req.AccessToken, req.PhoneNumberID, req.WABAID, req.BusinessID, ownerWorkspaceID, ownerAssignedBy)
		return
	}

	if code == "" {
		log.Printf("[meta-embedded-signup] ❌ No code or access_token provided")
		response.WriteError(w, http.StatusBadRequest, "no code or access_token provided", map[string]string{
			"code":         "string (exchangeable token code from FB.login)",
			"access_token": "string (alternative: direct access token)",
		})
		return
	}

	h.handleTokenExchange(w, code, req.PhoneNumberID, req.WABAID, req.BusinessID, ownerWorkspaceID, ownerAssignedBy)
}

func (h *MetaEmbeddedSignupHandler) handleTokenExchange(w http.ResponseWriter, code, phoneNumberID, wabaID, businessID, ownerWorkspaceID, ownerAssignedBy string) {
	log.Printf("[meta-embedded-signup] Exchanging code for token...")

	if h.appSecret == "" {
		log.Printf("[meta-embedded-signup] ⚠️ META_APP_SECRET not set!")
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"status":          "code_received_but_no_app_secret",
			"code":            code,
			"phone_number_id": phoneNumberID,
			"waba_id":         wabaID,
			"business_id":     businessID,
			"message":         "Code received but META_APP_SECRET is not configured. Set it to enable token exchange.",
		})
		return
	}

	tokenURL := fmt.Sprintf("https://graph.facebook.com/%s/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		h.graphAPIVer,
		url.QueryEscape(h.appID),
		url.QueryEscape(h.appSecret),
		url.QueryEscape(code),
	)

	tokenResp, err := h.httpClient.Get(tokenURL)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Token exchange HTTP error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "failed to exchange code for token", nil)
		return
	}
	defer tokenResp.Body.Close()

	tokenBody, _ := io.ReadAll(tokenResp.Body)

	if tokenResp.StatusCode != http.StatusOK {
		log.Printf("[meta-embedded-signup] ❌ Token exchange failed (status %d): %s", tokenResp.StatusCode, string(tokenBody))
		response.WriteError(w, http.StatusBadGateway, fmt.Sprintf("token exchange failed (status %d): %s", tokenResp.StatusCode, string(tokenBody)), nil)
		return
	}

	var tokenResult struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
		log.Printf("[meta-embedded-signup] ❌ Failed to parse token response: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "failed to parse token exchange response", nil)
		return
	}

	log.Printf("[meta-embedded-signup] ✅ Token exchange successful (expires_in=%ds)", tokenResult.ExpiresIn)
	h.processWithToken(w, tokenResult.AccessToken, phoneNumberID, wabaID, businessID, ownerWorkspaceID, ownerAssignedBy)
}

func (h *MetaEmbeddedSignupHandler) processWithToken(w http.ResponseWriter, accessToken, phoneNumberID, wabaID, businessID, ownerWorkspaceID, ownerAssignedBy string) {
	results := map[string]interface{}{
		"status":          "success",
		"phone_number_id": phoneNumberID,
		"waba_id":         wabaID,
		"business_id":     businessID,
	}

	debugResult := h.debugToken(accessToken)
	results["debug_token"] = debugResult

	if wabaID == "" && debugResult != nil {
		if data, ok := debugResult.(map[string]interface{}); ok {
			if d, ok := data["data"].(map[string]interface{}); ok {
				if scopes, ok := d["granular_scopes"].([]interface{}); ok {
					for _, scope := range scopes {
						if s, ok := scope.(map[string]interface{}); ok {
							if s["scope"] == "whatsapp_business_management" {
								if targets, ok := s["target_ids"].([]interface{}); ok && len(targets) > 0 {
									wabaID = fmt.Sprintf("%v", targets[0])
									log.Printf("[meta-embedded-signup] Extracted WABA ID from debug_token: %s", wabaID)
									results["waba_id"] = wabaID
								}
							}
						}
					}
				}
			}
		}
	}

	if phoneNumberID == "" && wabaID != "" {
		log.Printf("[meta-embedded-signup] Fetching phone numbers from WABA %s...", wabaID)
		fetchedPhoneID, fetchedPhoneNumber := h.getWABAPhoneNumbers(wabaID, accessToken)
		if fetchedPhoneID != "" {
			phoneNumberID = fetchedPhoneID
			results["phone_number_id"] = phoneNumberID
			log.Printf("[meta-embedded-signup] ✅ Found phone: %s (%s)", phoneNumberID, fetchedPhoneNumber)
		} else {
			log.Printf("[meta-embedded-signup] ⚠️ No phone numbers found in WABA %s", wabaID)
		}
	}

	if wabaID != "" {
		subscribeResult := h.subscribeToWebhooks(wabaID, accessToken)
		results["webhook_subscription"] = subscribeResult
	}

	var phoneStatus string
	var messagingLimitTier string
	var isCoexistence bool
	if phoneNumberID != "" {
		phoneDetails := h.getPhoneNumberDetails(phoneNumberID, accessToken)
		results["phone_details"] = phoneDetails
		if detailMap, ok := phoneDetails.(map[string]interface{}); ok {
			if s, ok := detailMap["status"].(string); ok {
				phoneStatus = s
			}
			if t, ok := detailMap["messaging_limit_tier"].(string); ok {
				messagingLimitTier = t
			}
		}

		if h.coexistenceService != nil {
			coexStatus, err := h.coexistenceService.CheckCoexistenceStatus(phoneNumberID, accessToken)
			if err != nil {
				log.Printf("[meta-embedded-signup] ⚠️ Could not check coexistence status: %v", err)
				results["coexistence_check"] = map[string]interface{}{"error": err.Error()}
			} else {
				isCoexistence = coexStatus.IsCoexistence()
				results["coexistence_check"] = map[string]interface{}{
					"is_on_biz_app":  coexStatus.IsOnBizApp,
					"platform_type":  coexStatus.PlatformType,
					"is_coexistence": isCoexistence,
				}
				log.Printf("[meta-embedded-signup] Coexistence check: is_on_biz_app=%v, platform_type=%s, coexistence=%v",
					coexStatus.IsOnBizApp, coexStatus.PlatformType, isCoexistence)
			}
		}
	}

	if phoneNumberID != "" {
		if isCoexistence {
			log.Printf("[meta-embedded-signup] Phone %s is in coexistence mode, skipping registration", phoneNumberID)
			results["phone_registration"] = "skipped - coexistence phone (already registered on WhatsApp Business App)"
		} else if phoneStatus == "CONNECTED" {
			log.Printf("[meta-embedded-signup] Phone %s is already CONNECTED, skipping registration", phoneNumberID)
			results["phone_registration"] = "skipped - already CONNECTED"
		} else {
			registerResult := h.registerPhoneNumber(phoneNumberID, accessToken)
			results["phone_registration"] = registerResult
		}
	}

	var wabaName, accountReviewStatus, businessVerificationStatus, ownershipType string
	if wabaID != "" {
		wabaDetails := h.getWABADetails(wabaID, accessToken)
		results["waba_details"] = wabaDetails
		if detailMap, ok := wabaDetails.(map[string]interface{}); ok {
			if n, ok := detailMap["name"].(string); ok {
				wabaName = n
			}
			if s, ok := detailMap["account_review_status"].(string); ok {
				accountReviewStatus = s
			}
			if s, ok := detailMap["business_verification_status"].(string); ok {
				businessVerificationStatus = s
			}
			if s, ok := detailMap["ownership_type"].(string); ok {
				ownershipType = s
			}
		}
	}

	if h.onboardUseCase != nil && phoneNumberID != "" && wabaID != "" && accessToken != "" {
		onboardResult, err := h.onboardUseCase.Execute(businessphone.OnboardEmbeddedSignupInput{
			Provider:                   businessphone.ProviderMeta,
			PhoneNumberID:              phoneNumberID,
			WABAId:                     wabaID,
			OwnerWorkspaceID:           ownerWorkspaceID,
			OwnerAssignedBy:            ownerAssignedBy,
			BusinessID:                 businessID,
			AccessToken:                accessToken,
			WABAName:                   wabaName,
			AccountReviewStatus:        accountReviewStatus,
			BusinessVerificationStatus: businessVerificationStatus,
			OwnershipType:              ownershipType,
			MessagingLimitTier:         messagingLimitTier,
			IsCoexistence:              isCoexistence,
		})
		if err != nil {
			log.Printf("[meta-embedded-signup] ❌ Failed to save phone to database: %v", err)
			results["db_save"] = map[string]interface{}{
				"error": err.Error(),
			}
		} else {
			log.Printf("[meta-embedded-signup] ✅ Phone saved to DB: id=%s, isNew=%v, waba=%s (%s)",
				onboardResult.Phone.ID, onboardResult.IsNew, wabaID, wabaName)
			results["db_save"] = map[string]interface{}{
				"success":  true,
				"phone_id": onboardResult.Phone.ID,
				"is_new":   onboardResult.IsNew,
				"status":   string(onboardResult.Phone.Status),
			}

			if isCoexistence && h.ensureOrganicCampaignUseCase != nil && h.coexistenceService != nil {
				h.handleCoexistenceSync(onboardResult.Phone, ownerWorkspaceID, phoneNumberID, accessToken, results)
			}
		}
	} else if phoneNumberID == "" || wabaID == "" {
		log.Printf("[meta-embedded-signup] ⚠️ Skipping DB save: phone=%s, waba=%s", phoneNumberID, wabaID)
		results["db_save"] = "skipped - missing required data"
	}

	response.WriteSuccess(w, http.StatusOK, results)
}

func (h *MetaEmbeddedSignupHandler) handleCoexistenceSync(
	phone *businessphone.WhatsAppBusinessPhoneNumber,
	workspaceID, metaPhoneNumberID, accessToken string,
	results map[string]interface{},
) {
	coexResults := map[string]interface{}{}

	campaign, created, err := h.ensureOrganicCampaignUseCase.Execute(workspaceID, phone.ID, phone.DisplayPhoneNumber)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Failed to create organic campaign: %v", err)
		coexResults["organic_campaign"] = map[string]interface{}{"error": err.Error()}
		results["coexistence_sync"] = coexResults
		return
	}
	if created {
		log.Printf("[meta-embedded-signup] ✅ Created organic campaign %s for coexistence phone %s", campaign.ID, phone.ID)
	} else {
		log.Printf("[meta-embedded-signup] Organic campaign %s already exists for phone %s", campaign.ID, phone.ID)
	}
	coexResults["organic_campaign"] = map[string]interface{}{
		"id":      campaign.ID,
		"created": created,
	}

	contactsResp, err := h.coexistenceService.InitiateContactsSync(metaPhoneNumberID, accessToken)
	if err != nil {
		log.Printf("[meta-embedded-signup] ⚠️ Contacts sync failed: %v", err)
		coexResults["contacts_sync"] = map[string]interface{}{"error": err.Error()}
	} else {
		log.Printf("[meta-embedded-signup] ✅ Contacts sync initiated: request_id=%s", contactsResp.RequestID)
		coexResults["contacts_sync"] = map[string]interface{}{
			"request_id": contactsResp.RequestID,
			"status":     "initiated",
		}
	}

	historyResp, err := h.coexistenceService.InitiateHistorySync(metaPhoneNumberID, accessToken)
	if err != nil {
		log.Printf("[meta-embedded-signup] ⚠️ History sync failed: %v (business may have declined sharing)", err)
		coexResults["history_sync"] = map[string]interface{}{"error": err.Error()}
	} else {
		log.Printf("[meta-embedded-signup] ✅ History sync initiated: request_id=%s", historyResp.RequestID)
		coexResults["history_sync"] = map[string]interface{}{
			"request_id": historyResp.RequestID,
			"status":     "initiated",
		}
	}

	results["coexistence_sync"] = coexResults
}

func (h *MetaEmbeddedSignupHandler) debugToken(accessToken string) interface{} {
	appAccessToken := h.appID + "|" + h.appSecret
	debugURL := fmt.Sprintf("https://graph.facebook.com/%s/debug_token?input_token=%s&access_token=%s",
		h.graphAPIVer,
		url.QueryEscape(accessToken),
		url.QueryEscape(appAccessToken),
	)

	resp, err := h.httpClient.Get(debugURL)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Debug token error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[meta-embedded-signup] Debug token: status=%d", resp.StatusCode)

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]string{"raw": string(body)}
	}
	return result
}

func (h *MetaEmbeddedSignupHandler) subscribeToWebhooks(wabaID, accessToken string) interface{} {
	subscribeURL := fmt.Sprintf("https://graph.facebook.com/%s/%s/subscribed_apps",
		h.graphAPIVer, wabaID)

	fields := strings.Join(businessphone.SubscriptionWebhookFields(), ",")

	formData := url.Values{}
	formData.Set("subscribed_fields", fields)

	req, err := http.NewRequest(http.MethodPost, subscribeURL, strings.NewReader(formData.Encode()))
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Subscribe request error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Subscribe HTTP error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[meta-embedded-signup] Webhook subscribe: status=%d", resp.StatusCode)

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]string{"raw": string(body)}
	}
	return result
}

func (h *MetaEmbeddedSignupHandler) registerPhoneNumber(phoneNumberID, accessToken string) interface{} {
	registerURL := fmt.Sprintf("https://graph.facebook.com/%s/%s/register",
		h.graphAPIVer, phoneNumberID)

	payload := `{"messaging_product":"whatsapp","pin":"000000"}`

	req, err := http.NewRequest(http.MethodPost, registerURL, strings.NewReader(payload))
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Register request error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Register HTTP error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[meta-embedded-signup] Register phone: status=%d", resp.StatusCode)

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]string{"raw": string(body)}
	}
	return result
}

func (h *MetaEmbeddedSignupHandler) getWABAPhoneNumbers(wabaID, accessToken string) (string, string) {
	phonesURL := fmt.Sprintf("https://graph.facebook.com/%s/%s/phone_numbers",
		h.graphAPIVer, wabaID)

	req, err := http.NewRequest(http.MethodGet, phonesURL, nil)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Phone numbers request error: %v", err)
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Phone numbers HTTP error: %v", err)
		return "", ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[meta-embedded-signup] ❌ Failed to fetch phone numbers (status %d)", resp.StatusCode)
		return "", ""
	}

	var result struct {
		Data []struct {
			ID                 string `json:"id"`
			DisplayPhoneNumber string `json:"display_phone_number"`
			VerifiedName       string `json:"verified_name"`
			QualityRating      string `json:"quality_rating"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[meta-embedded-signup] ❌ Failed to parse phone numbers response: %v", err)
		return "", ""
	}

	if len(result.Data) == 0 {
		return "", ""
	}

	phone := result.Data[0]
	log.Printf("[meta-embedded-signup] Found %d phone(s) in WABA, using: %s (%s)", len(result.Data), phone.ID, phone.DisplayPhoneNumber)
	return phone.ID, phone.DisplayPhoneNumber
}

func (h *MetaEmbeddedSignupHandler) getPhoneNumberDetails(phoneNumberID, accessToken string) interface{} {
	detailsURL := fmt.Sprintf("https://graph.facebook.com/%s/%s?fields=id,display_phone_number,verified_name,code_verification_status,quality_rating,platform_type,throughput,is_official_business_account,account_mode,is_pin_enabled,name_status,new_name_status,status,search_visibility,messaging_limit_tier",
		h.graphAPIVer, phoneNumberID)

	req, err := http.NewRequest(http.MethodGet, detailsURL, nil)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Phone details request error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ Phone details HTTP error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[meta-embedded-signup] Phone details: status=%d", resp.StatusCode)

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]string{"raw": string(body)}
	}
	return result
}

func (h *MetaEmbeddedSignupHandler) getWABADetails(wabaID, accessToken string) interface{} {
	detailsURL := fmt.Sprintf("https://graph.facebook.com/%s/%s?fields=id,name,currency,timezone_id,message_template_namespace,account_review_status,business_verification_status,ownership_type",
		h.graphAPIVer, wabaID)

	req, err := http.NewRequest(http.MethodGet, detailsURL, nil)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ WABA details request error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		log.Printf("[meta-embedded-signup] ❌ WABA details HTTP error: %v", err)
		return map[string]string{"error": err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Printf("[meta-embedded-signup] WABA details: status=%d", resp.StatusCode)

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]string{"raw": string(body)}
	}
	return result
}

// @Summary		Repetir handover do 360dialog
// @Description	Reexecuta um handover do 360dialog que falhou anteriormente para um número pertencente ao workspace.
// @Tags			Cadastro incorporado do WhatsApp
// @Produce		json
// @Param			workspace_id	query		string	true	"Identificador do workspace proprietário do número"
// @Param			phone_id		query		string	true	"Identificador do número a ser reprocessado"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		502	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/onboard/360dialog/retry [post]
func (h *MetaEmbeddedSignupHandler) HandleDialog360Retry(w http.ResponseWriter, r *http.Request) {
	if h.dialog360 == nil {
		response.WriteError(w, http.StatusNotFound, "360dialog onboarding is not enabled", nil)
		return
	}
	workspaceID, _, ok := h.requireOwnerContext(w, r)
	if !ok {
		return
	}
	phoneID := strings.TrimSpace(r.URL.Query().Get("phone_id"))
	if phoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "phone_id is required", nil)
		return
	}
	phone, err := h.dialog360.Retry(phoneID, workspaceID)
	if err != nil {
		details := map[string]string{}
		if phone != nil {
			details["status"] = string(phone.Status)
		}
		response.WriteError(w, http.StatusBadGateway, "retry failed: "+err.Error(), details)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"status":   "success",
		"phone_id": phone.ID,
		"state":    string(phone.Status),
	})
}

func (h *MetaEmbeddedSignupHandler) HandleDialog360PartnerWebhook(w http.ResponseWriter, r *http.Request) {
	if h.dialog360 == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !h.verifyDialog360Webhook(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	for _, channelID := range parseDialog360LiveChannels(body) {
		if ferr := h.dialog360.Finalize(channelID, time.Now().UTC()); ferr != nil {
			log.Printf("[dialog360-webhook] finalize channel %s failed: %v", channelID, ferr)
		}
	}

	for _, name := range parseDialog360PartnerEventNames(body) {
		switch name {
		case "channel_live", "channel_running":
		case "waba_template_status_changed":
		default:
			log.Printf("[dialog360-webhook] partner event %q received (not yet routed to internal pipeline)", name)
		}
	}

	if h.templateWebhook != nil {
		for _, p := range parseDialog360TemplateStatusPayloads(body) {
			if err := h.templateWebhook.Execute(p); err != nil {
				log.Printf("[dialog360-webhook] template status update failed: %v", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *MetaEmbeddedSignupHandler) verifyDialog360Webhook(r *http.Request) bool {
	secret := strings.TrimSpace(h.dialog360WebhookSecret)
	if secret == "" {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get("X-360Dialog-Webhook-Secret"))
	if provided == "" {
		provided = strings.TrimSpace(r.URL.Query().Get("secret"))
	}
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}
