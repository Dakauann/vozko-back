package lead

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

type Locations interface {
	PinLocation(ctx context.Context, a lead_usecase.Actor, leadID, addressID string, point geo.Point) (*leaddomain.Lead, error)
	AcceptLocation(ctx context.Context, a lead_usecase.Actor, leadID, messageID string) (*leaddomain.Lead, error)
}

func (h *LeadHandler) locationActor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	if h.locations == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_locations_unavailable", "A localização de leads não está disponível neste servidor", nil)
		return lead_usecase.Actor{}, false
	}
	return h.requestActor(w, r)
}

// @Summary		Usar a localização que o lead mandou
// @Description	Grava como posição do lead a localização que ele mandou numa conversa ("Usar como localização de Maria"). Nada é gravado sem este clique. A mensagem precisa ser uma localização recebida do lead (WhatsApp oficial, coexistência ou Telegram; nunca uma que a equipe mandou), dentro do Brasil, numa conversa que pertence a este lead, neste workspace, e que quem pede consegue abrir (mesma regra de acesso da caixa de entrada); qualquer outra mensagem responde 404 `lead_location_not_found`, sem dizer qual das condições falhou. A posição entra no endereço principal com precisão `exact` e origem `lead_pin` (status `located`); um lead sem endereço ganha um endereço principal `home` só com a posição. A posição substitui a anterior, inclusive um alfinete posto à mão, e nenhuma localização automática a troca depois. Exige `leads:update`; não exige `leads:read_addresses`, e a resposta segue a regra de visibilidade (quem não lê endereços vê só bairro, cidade e o status). Não exige `If-Match`; aceitar de novo a mesma localização não muda nada. Grava o evento `location_accepted` no histórico do lead (com rótulo, precisão e origem, sem coordenadas) e avisa por `conversation:lead_update` com o campo `addresses`. As mensagens que podem ser aceitas trazem `location.candidate = true` no histórico da conversa.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead (UUID)"
// @Param			messageId	path		string	true	"ID da mensagem de localização (UUID)"
// @Success		200			{object}	lead.LeadRecordResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/location-candidates/{messageId}/accept [post]
func (h *LeadHandler) AcceptLocation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.locationActor(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	accepted, err := h.locations.AcceptLocation(r.Context(), a, vars["id"], vars["messageId"])
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(accepted, h.now()))
}

// @Summary		Pôr o alfinete do endereço à mão
// @Description	Grava a posição de um endereço do lead pelo mapa ("arrastar o alfinete"). A posição entra com precisão `exact` e origem `manual` (status `located`), substitui a anterior e nenhuma localização automática a troca depois; editar o texto do endereço mantém o alfinete só quando quem edita pede para mantê-lo. O ponto precisa ser válido e ficar dentro do Brasil (400 `lead_location_invalid`). O endereço precisa ser deste lead (404 `lead_address_not_found`). Corpo estrito: `latitude` e `longitude` obrigatórias; outra chave dá 400 `invalid_body`. Exige `leads:update` e `leads:read_addresses` (403 `forbidden`). Não exige `If-Match`; repetir o mesmo ponto não muda nada. Grava o evento `location_pinned` no histórico do lead (sem coordenadas) e avisa por `conversation:lead_update` com o campo `addresses`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id			path		string					true	"ID do lead (UUID)"
// @Param			addressId	path		string					true	"ID do endereço (UUID)"
// @Param			request		body		PinLeadLocationRequest	true	"Posição do alfinete"
// @Success		200			{object}	lead.LeadRecordResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/addresses/{addressId}/pin [post]
func (h *LeadHandler) PinLocation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.locationActor(w, r)
	if !ok {
		return
	}
	var req PinLeadLocationRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	if req.Latitude == nil || req.Longitude == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"latitude":  "number (required, decimal degrees)",
			"longitude": "number (required, decimal degrees)",
		})
		return
	}
	vars := mux.Vars(r)
	pinned, err := h.locations.PinLocation(r.Context(), a, vars["id"], vars["addressId"], geo.Point{Lat: *req.Latitude, Lng: *req.Longitude})
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadRecord(pinned, h.now()))
}
