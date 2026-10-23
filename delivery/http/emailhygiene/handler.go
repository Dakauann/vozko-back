package emailhygienehttp

import (
	"context"
	"log"
	"net/http"
	"time"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	hyuc "vozko/usecases/emailhygiene"
)

type StatusService interface {
	AddressStatus(ctx context.Context, workspaceID, raw string) (hyuc.AddressStatus, error)
}

type Handler struct {
	status StatusService
}

func NewHandler(status StatusService) *Handler {
	if status == nil {
		panic("email hygiene http: the address status service is required")
	}
	return &Handler{status: status}
}

var (
	refusalInvalid     = response.Refusal{Status: http.StatusUnprocessableEntity, Code: "email_address_invalid", Message: "Informe um endereço de e-mail válido."}
	refusalUnavailable = response.Refusal{Status: http.StatusServiceUnavailable, Code: "email_address_status_unavailable", Message: "Não foi possível consultar a situação deste endereço agora. Tente de novo em instantes."}
	refusals           = []response.ErrorRefusal{{Err: hyuc.ErrAddressInvalid, Refusal: refusalInvalid}}
)

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(refusals), []response.Refusal{refusalUnavailable})
}

// @Summary		Consultar a situação de um endereço de e-mail
// @Description	Diz se o endereço pode receber campanhas e por quê. `verdict` é `valid`, `suppressed` (na lista de bloqueio deste workspace), `bad_address` (voltou como inexistente em algum envio da plataforma), `no_mail_domain` (o domínio não recebe e-mails), `disposable` (e-mail temporário), `domain_unknown` (o domínio não respondeu à consulta), `domain_pending` (o domínio ainda está sendo verificado), `inactive` (sem interação há mais de 6 meses depois de 5 campanhas), `typo` (provável erro de digitação, com a sugestão em `suggestion`) ou `role` (endereço de setor, como contato@).
// @Description	`typo` e `role` são avisos: o envio continua. Um domínio nunca verificado entra na fila de verificação e volta como `domain_pending`; consulte de novo em instantes. A consulta não faz nenhuma busca de DNS durante a requisição.
// @Tags			E-mail
// @Produce		json
// @Param			address	query		string	true	"Endereço de e-mail"
// @Success		200		{object}	AddressStatusResponse
// @Failure		422		{object}	response.ErrorResponse	"email_address_invalid"
// @Failure		503		{object}	response.ErrorResponse	"email_address_status_unavailable"
// @Security		BearerAuth
// @Router			/email/address-status [get]
func (hd *Handler) AddressStatus(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	status, err := hd.status.AddressStatus(r.Context(), ws, r.URL.Query().Get("address"))
	if err != nil {
		if response.WriteErrorRefusal(w, err, refusals) {
			return
		}
		log.Printf("[email-hygiene] address status for workspace %s: %v", ws, err)
		response.WriteRefusal(w, refusalUnavailable)
		return
	}
	response.WriteSuccess(w, http.StatusOK, present(status))
}

type DomainStatus struct {
	State string `json:"state" enums:"pending,accepts,no_mail,unknown"`
	Stale bool   `json:"stale"`
}

type SuppressionStatus struct {
	Reason       string     `json:"reason"`
	Scope        string     `json:"scope"`
	SuppressedAt *time.Time `json:"suppressed_at"`
}

type AddressStatusResponse struct {
	Address          string             `json:"address"`
	Verdict          string             `json:"verdict" enums:"valid,suppressed,bad_address,no_mail_domain,disposable,domain_unknown,domain_pending,inactive,typo,role"`
	BlocksCampaigns  bool               `json:"blocks_campaigns"`
	Domain           DomainStatus       `json:"domain"`
	Disposable       bool               `json:"disposable"`
	Role             bool               `json:"role"`
	Suggestion       string             `json:"suggestion"`
	SuggestedAddress string             `json:"suggested_address"`
	Suppression      *SuppressionStatus `json:"suppression"`
	SoftBounces      int                `json:"soft_bounces"`
	FirstDeliveredAt *time.Time         `json:"first_delivered_at"`
	LastEngagedAt    *time.Time         `json:"last_engaged_at"`
	Inactive         bool               `json:"inactive"`
	NeverMailed      bool               `json:"never_mailed"`
}

const (
	scopeWorkspace = "workspace"
	scopeShared    = "shared"
)

func present(s hyuc.AddressStatus) AddressStatusResponse {
	out := AddressStatusResponse{
		Address:          s.Address.String(),
		Verdict:          string(s.Verdict),
		BlocksCampaigns:  s.Verdict.BlocksCampaigns(),
		Domain:           DomainStatus{State: string(s.Standing.Domain), Stale: s.Standing.DomainStale},
		Disposable:       s.Facts.Disposable,
		Role:             s.Facts.Role,
		Suggestion:       s.Facts.Suggestion,
		SuggestedAddress: s.Suggested.String(),
		SoftBounces:      s.Standing.Activity.SoftStreak,
		FirstDeliveredAt: s.Standing.Activity.FirstDeliveredAt,
		LastEngagedAt:    s.Standing.Activity.LastEngagedAt,
		Inactive:         s.Inactive,
		NeverMailed:      !s.Standing.Activity.EverDelivered(),
	}
	switch {
	case s.Standing.Suppression != "":
		out.Suppression = &SuppressionStatus{Reason: string(s.Standing.Suppression), Scope: scopeWorkspace, SuppressedAt: s.Standing.SuppressedAt}
	case s.Standing.BadAddress:
		out.Suppression = &SuppressionStatus{Reason: "hard_bounce", Scope: scopeShared}
	}
	return out
}
