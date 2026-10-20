package mercadolivre

import (
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	mluc "vozko/usecases/mercadolivre"
)

const maxNotificationBody = 64 << 10

type NotificationIntake interface {
	AcceptWithToken(token string, raw []byte) error
}

type NotificationHandler struct {
	intake NotificationIntake
}

func NewNotificationHandler(intake NotificationIntake) *NotificationHandler {
	if intake == nil {
		panic("mercadolivre http: the notification handler requires the intake")
	}
	return &NotificationHandler{intake: intake}
}

// @Summary		Notificações do Mercado Livre
// @Description	Recebe as notificações do Mercado Livre (questions, messages, orders_v2, shipments, items). O token do caminho é comparado em tempo constante; a notificação só é validada e enfileirada, sem acesso ao banco, e responde em bem menos de 500 ms.
// @Tags			Mercado Livre
// @Accept			json
// @Produce		json
// @Param			token	path		string	true	"Token secreto do caminho (MERCADOLIVRE_NOTIFICATION_TOKEN)"
// @Success		200		{object}	NotificationAck
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404
// @Failure		413		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Router			/webhooks/mercadolivre/{token} [post]
func (h *NotificationHandler) Handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxNotificationBody+1))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Unreadable body", nil)
		return
	}
	if len(body) > maxNotificationBody {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "Notification too large", nil)
		return
	}
	switch err := h.intake.AcceptWithToken(mux.Vars(r)["token"], body); {
	case err == nil:
		response.WriteSuccess(w, http.StatusOK, NotificationAck{Status: "accepted"})
	case errors.Is(err, mluc.ErrNotificationToken):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, mluc.ErrNotificationRefused):
		response.WriteError(w, http.StatusBadRequest, "Notification refused", nil)
	default:
		response.WriteError(w, http.StatusServiceUnavailable, "Notification queue unavailable", nil)
	}
}
