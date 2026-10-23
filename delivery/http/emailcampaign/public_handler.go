package emailcampaignhttp

import (
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/brand"
	"vozko/delivery/http/response"
	"vozko/domain/emailconsent"
)

const (
	oneClickBody     = "List-Unsubscribe=One-Click"
	confirmField     = "confirm"
	confirmValue     = "link"
	maxPublicBody    = 4 << 10
	publicPagePrefix = "/public/email"
)

type pageState string

const (
	pageConfirm pageState = "confirm"
	pageDone    pageState = "done"
	pageInvalid pageState = "invalid"
	pagePreview pageState = "preview"
)

type pageData struct {
	Brand  string
	State  pageState
	Action string
}

var publicPage = template.Must(template.New("unsubscribe").Parse(`<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Brand}}</title>
<style>
body{margin:0;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;background:#f4f4f5;color:#18181b}
main{max-width:480px;margin:64px auto;padding:32px 24px;background:#fff;border-radius:12px}
h1{font-size:22px;margin:0 0 12px}
p{line-height:1.5;margin:0 0 20px}
button{font:inherit;padding:12px 20px;border:0;border-radius:8px;background:#18181b;color:#fff;cursor:pointer}
small{color:#71717a}
</style>
</head>
<body>
<main>
{{if eq .State "confirm"}}
<h1>Deixar de receber estes e-mails?</h1>
<p>Confirme abaixo e você não receberá mais e-mails de marketing deste remetente.</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="confirm" value="link">
<button type="submit">Confirmar descadastro</button>
</form>
{{else if eq .State "done"}}
<h1>Pronto, você foi descadastrado</h1>
<p>Você não receberá mais e-mails de marketing deste remetente.</p>
{{else if eq .State "preview"}}
<h1>Pré-visualização</h1>
<p>Este link aparece apenas nas pré-visualizações. Nos e-mails enviados ele leva à página de descadastro do destinatário.</p>
{{else}}
<h1>Link inválido</h1>
<p>Este link de descadastro não é válido. Use o link do e-mail mais recente que você recebeu.</p>
{{end}}
<small>Enviado com {{.Brand}}</small>
</main>
</body>
</html>
`))

type PublicHandler struct {
	consent Consent
}

func NewPublicHandler(consent Consent) *PublicHandler {
	return &PublicHandler{consent: consent}
}

func writePage(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	data.Brand = brand.Active().Name
	_ = publicPage.Execute(w, data)
}

// @Summary		Página de descadastro
// @Description	Mostra a confirmação do descadastro sem descadastrar (leitores de link e antivírus abrem links com GET). Token inválido mostra a página de link inválido.
// @Tags			E-mail marketing
// @Produce		html
// @Param			token	path	string	true	"Token de descadastro"
// @Success		200
// @Failure		400
// @Router			/public/email/unsubscribe/{token} [get]
func (h *PublicHandler) UnsubscribePage(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]
	if err := h.consent.CheckToken(token); err != nil {
		writePage(w, http.StatusBadRequest, pageData{State: pageInvalid})
		return
	}
	writePage(w, http.StatusOK, pageData{State: pageConfirm, Action: publicPagePrefix + "/unsubscribe/" + token})
}

// @Summary		Descadastrar (um clique)
// @Description	Alvo do List-Unsubscribe-Post (RFC 8058): o corpo `List-Unsubscribe=One-Click` descadastra e responde 200 sem corpo. O formulário da página de confirmação (`confirm=link`) descadastra e mostra a página de sucesso. Repetir não muda nada.
// @Tags			E-mail marketing
// @Accept			x-www-form-urlencoded
// @Produce		html
// @Param			token	path	string	true	"Token de descadastro"
// @Success		200
// @Failure		400	{object}	response.ErrorResponse	"email_unsubscribe_token_invalid"
// @Router			/public/email/unsubscribe/{token} [post]
func (h *PublicHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := mux.Vars(r)["token"]
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxPublicBody))
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "email_request_invalid", "Corpo da requisição inválido", nil)
		return
	}
	body := string(raw)
	if strings.Contains(body, oneClickBody) {
		if _, err := h.consent.Unsubscribe(r.Context(), token, emailconsent.UnsubscribeOneClick); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if !strings.Contains(body, confirmField+"="+confirmValue) {
		writePage(w, http.StatusBadRequest, pageData{State: pageInvalid})
		return
	}
	if _, err := h.consent.Unsubscribe(r.Context(), token, emailconsent.UnsubscribeLink); err != nil {
		if r, ok := classify(err); ok && r.status < http.StatusInternalServerError {
			writePage(w, http.StatusBadRequest, pageData{State: pageInvalid})
			return
		}
		writeError(w, err)
		return
	}
	writePage(w, http.StatusOK, pageData{State: pageDone})
}

// @Summary		Destino do descadastro nas pré-visualizações
// @Description	Página informativa usada como link de descadastro nas pré-visualizações do editor. Não altera nada.
// @Tags			E-mail marketing
// @Produce		html
// @Success		200
// @Router			/public/email/preview [get]
func (h *PublicHandler) Preview(w http.ResponseWriter, _ *http.Request) {
	writePage(w, http.StatusOK, pageData{State: pagePreview})
}
