package advertisinghttp

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type AdActivityResponse struct {
	EventType  string    `json:"eventType"`
	Label      string    `json:"label"`
	At         time.Time `json:"at"`
	ActorName  string    `json:"actorName"`
	ObjectID   string    `json:"objectId"`
	ObjectName string    `json:"objectName"`
	ObjectType string    `json:"objectType"`
	From       string    `json:"from"`
	To         string    `json:"to"`
}

func presentActivities(activities []advertising.AdActivity) []AdActivityResponse {
	return presentAll(activities, func(a advertising.AdActivity) AdActivityResponse {
		return AdActivityResponse{
			EventType: a.EventType, Label: a.Label, At: a.At, ActorName: a.ActorName,
			ObjectID: a.ObjectID, ObjectName: a.ObjectName, ObjectType: a.ObjectType, From: a.From, To: a.To,
		}
	})
}

// @Summary		Histórico de atividade de um item
// @Description	O que mudou na campanha, conjunto ou anúncio e nos itens dentro dele (status, orçamento, público, criativo, análise da Meta), quem mudou e quando, no texto da própria Meta no idioma pedido. Os 200 registros mais recentes do período; sem período, os últimos 30 dias.
// @Tags			Anúncios
// @Produce		json
// @Param			metaId	path		string	true	"ID do item na Meta"
// @Param			since	query		string	false	"YYYY-MM-DD"
// @Param			until	query		string	false	"YYYY-MM-DD"
// @Param			locale	query		string	false	"pt (padrão), en, es ou de"
// @Success		200		{array}		AdActivityResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/history [get]
func (h *Handler) ObjectHistory(w http.ResponseWriter, r *http.Request) {
	dates, err := dateRangeQuery(r.URL.Query())
	if err != nil {
		writeError(w, err, "Failed to load the ad history")
		return
	}
	activities, err := h.d.History.List(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], dates, r.URL.Query().Get("locale"))
	if err != nil {
		writeError(w, err, "Failed to load the ad history")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentActivities(activities))
}
