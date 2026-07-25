package shortlink

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	shortlinkdomain "vozko/domain/shortlink"
	"vozko/infra/http/middleware"
)

var passwordPageTemplate = template.Must(template.New("shortlink_password").Parse(`<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Link protegido</title>
<style>
:root { color-scheme: light dark; }
* { box-sizing: border-box; }
body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center; background:#f4f5f7; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif; color:#0f172a; }
.card { width:100%; max-width:380px; margin:24px; padding:32px; background:#ffffff; border:1px solid #e2e8f0; border-radius:16px; box-shadow:0 10px 30px rgba(15,23,42,.06); }
.badge { width:44px; height:44px; border-radius:12px; background:#2463eb; display:flex; align-items:center; justify-content:center; margin-bottom:20px; }
.badge svg { width:22px; height:22px; }
h1 { font-size:18px; margin:0 0 6px; }
p { font-size:14px; color:#64748b; margin:0 0 20px; }
label { display:block; font-size:13px; font-weight:600; margin-bottom:8px; }
input { width:100%; padding:12px 14px; font-size:15px; border:1px solid #cbd5e1; border-radius:10px; background:#fff; color:#0f172a; }
input:focus { outline:none; border-color:#2463eb; box-shadow:0 0 0 3px rgba(36,99,235,.15); }
button { width:100%; margin-top:16px; padding:12px 14px; font-size:15px; font-weight:600; color:#fff; background:#2463eb; border:none; border-radius:10px; cursor:pointer; }
button:hover { background:#1d4ed8; }
.error { margin-top:14px; font-size:13px; color:#dc2626; }
</style>
</head>
<body>
<main class="card">
<div class="badge"><svg viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg></div>
<h1>Este link é protegido</h1>
<p>Informe a senha para continuar até o destino.</p>
<form method="post" action="{{.Action}}">
<label for="password">Senha</label>
<input id="password" name="password" type="password" autocomplete="off" autofocus required>
<button type="submit">Acessar link</button>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
</form>
</main>
</body>
</html>`))

type passwordPageData struct {
	Action string
	Error  string
}

// Redirect godoc
// @Summary		Redirecionar um link curto
// @Description	Resolve o código público e redireciona para a URL de destino, exibe a página de senha quando o link é protegido, ou retorna 404. O clique é contabilizado de forma assíncrona.
// @Tags			Links Curtos
// @Produce		html
// @Param			code	path		string	true	"Código do link curto"
// @Success		302		"Redirecionamento para a URL de destino"
// @Success		200		{string}	string	"Página de senha (link protegido)"
// @Failure		404		{string}	string	"Link não encontrado ou expirado"
// @Router			/r/{code} [get]
func (h *ShortLinkHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]

	resolved, err := h.resolveUC.Execute(r.Context(), code)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	switch resolved.State {
	case shortlinkdomain.ResolveOK:
		h.enqueueClick(r, resolved)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, resolved.TargetURL, resolved.RedirectType.HTTPStatus())
	case shortlinkdomain.ResolvePassword:
		h.renderPasswordPage(w, code, "", http.StatusOK)
	default:
		h.renderNotFound(w)
	}
}

// Unlock godoc
// @Summary		Desbloquear um link curto protegido por senha
// @Description	Valida a senha de um link protegido e redireciona para a URL de destino em caso de sucesso. Se a senha estiver incorreta, reexibe a página de senha.
// @Tags			Links Curtos
// @Accept			x-www-form-urlencoded
// @Produce		html
// @Param			code		path		string	true	"Código do link curto"
// @Param			password	formData	string	true	"Senha do link"
// @Success		302			"Redirecionamento para a URL de destino"
// @Failure		401			{string}	string	"Senha incorreta"
// @Failure		404			{string}	string	"Link não encontrado"
// @Router			/r/{code}/unlock [post]
func (h *ShortLinkHandler) Unlock(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	password := r.FormValue("password")

	resolved, err := h.unlockUC.Execute(r.Context(), code, password)
	if err != nil {
		switch {
		case errors.Is(err, shortlinkdomain.ErrInvalidPassword), errors.Is(err, shortlinkdomain.ErrPasswordRequired):
			h.renderPasswordPage(w, code, "Senha incorreta. Tente novamente.", http.StatusUnauthorized)
		case errors.Is(err, shortlinkdomain.ErrShortLinkNotFound):
			h.renderNotFound(w)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	h.enqueueClick(r, resolved)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, resolved.TargetURL, resolved.RedirectType.HTTPStatus())
}

func (h *ShortLinkHandler) enqueueClick(r *http.Request, resolved *shortlinkdomain.ResolvedLink) {
	country, region, city := geoHints(r)
	query := r.URL.Query()

	msg := shortlinkdomain.ClickMessage{
		ClickEventID: uuid.New().String(),
		ShortLinkID:  resolved.ShortLinkID,
		WorkspaceID:  resolved.WorkspaceID,
		Code:         resolved.Code,
		OccurredAt:   time.Now().UTC(),
		IP:           middleware.GetClientIP(r),
		UserAgent:    r.UserAgent(),
		Referer:      r.Referer(),
		Language:     primaryLanguage(r.Header.Get("Accept-Language")),
		GeoCountry:   country,
		GeoRegion:    region,
		GeoCity:      city,
		UTMSource:    query.Get("utm_source"),
		UTMMedium:    query.Get("utm_medium"),
		UTMCampaign:  query.Get("utm_campaign"),
		Attempt:      1,
	}
	_ = h.publishUC.Execute(r.Context(), msg)
}

func (h *ShortLinkHandler) renderPasswordPage(w http.ResponseWriter, code, errMsg string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = passwordPageTemplate.Execute(w, passwordPageData{
		Action: "/r/" + url.PathEscape(code) + "/unlock",
		Error:  errMsg,
	})
}

func (h *ShortLinkHandler) renderNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\"><title>Link não encontrado</title><p>Este link não existe ou não está mais disponível.</p>"))
}

func geoHints(r *http.Request) (country, region, city string) {
	country = firstNonEmpty(r.Header.Get("CF-IPCountry"), r.Header.Get("X-Vercel-IP-Country"))
	region = firstNonEmpty(r.Header.Get("CF-Region-Code"), r.Header.Get("X-Vercel-IP-Country-Region"))
	city = firstNonEmpty(r.Header.Get("CF-IPCity"), r.Header.Get("X-Vercel-IP-City"))
	return country, region, city
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func primaryLanguage(acceptLanguage string) string {
	acceptLanguage = strings.TrimSpace(acceptLanguage)
	if acceptLanguage == "" {
		return ""
	}
	first, _, _ := strings.Cut(acceptLanguage, ",")
	first, _, _ = strings.Cut(first, ";")
	return strings.TrimSpace(first)
}
