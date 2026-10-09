package workspaceconfig

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/geocoding"
	"vozko/domain/georef"
	wsc "vozko/domain/workspace_config"
	workspace_config_usecase "vozko/usecases/workspace_config"
)

type GeocodingSettingsService interface {
	Get(ctx context.Context, workspaceID string, editor wsc.Caller) (workspace_config_usecase.GeocodingSettingsView, error)
	Change(ctx context.Context, workspaceID string, editor wsc.Caller, change geocoding.Change) (workspace_config_usecase.GeocodingSettingsView, error)
}

func (h *WorkspaceConfigHandler) SetGeocodingSettings(service GeocodingSettingsService) {
	h.geocoding = service
}

// @Summary		Ler a geolocalização de endereços do workspace
// @Description	Mostra como os endereços dos leads viram posições no mapa. A base de referência local (IBGE, CNEFE 2022: ponto do CEP, do bairro e da cidade) vale para todos os workspaces e não sai dos nossos servidores. Um provedor externo (`opencage`) é opcional: só é chamado quando o workspace optou por ele (registrado com quem e quando em `providerChangedBy`, `providerChangedByName` e `providerChangedAt`), só quando a referência não chega ao nível da rua e o endereço tem logradouro, e só dentro do teto mensal (`monthlyCeiling`, padrão 5.000 consultas por ciclo; 0 com o provedor desligado, o que significa nenhuma consulta externa) e da cota diária (`dailyShare`, o dobro de uma divisão igual do teto pelos dias do ciclo). `usedThisCycle` e `usedToday` contam as consultas feitas no ciclo (`cycleStart` até `nextCycleStart`, horário de Brasília). `availableProviders` lista os provedores que este servidor consegue usar; `attribution` é a fonte a citar para as posições da referência. `canChangeProvider` é verdadeiro para o dono do workspace e administradores da plataforma; `canChangeCeiling` só para administradores da plataforma. `providerPause` diz se o provedor externo está pausado para todos os workspaces (vem `null` quando não está, ou quando o workspace não usa provedor): quando o provedor recusa a conta da plataforma (chave rejeitada `key_rejected`, cota da conta esgotada `account_quota_spent`, chave desativada `key_disabled`, outra recusa da conta `account_refused`; ou `queries_refused` quando textos diferentes são recusados no mesmo lote, sinal de que o problema é a requisição e não o endereço), nenhuma consulta é feita e nenhuma cota é gasta até `until`, e os endereços esperam com a posição da referência; `state` `paused` traz o motivo, desde quando (`since`) e até quando (`until`); `state` `unknown` quer dizer que o estado da pausa não pôde ser lido, e enquanto isso nenhuma consulta externa é feita. Um endereço cujo texto o provedor recusa (consulta inválida ou longa demais) fica com `geoStatus` `refused` e só volta a ser consultado quando o texto muda; respostas do provedor são guardadas por workspace e texto de endereço, então o mesmo texto não é pago duas vezes. Qualquer membro do workspace pode ler. Recusas: 401 sem sessão, 403 `geocoding_forbidden` para quem não é membro (ou um workspace inexistente, ou um id que não é UUID), 500 `geocoding_settings_unreadable` quando a configuração ou o consumo não podem ser lidos (nunca responde "zero consultas" no lugar de um erro), 503 `geocoding_unavailable` quando a rota não está ligada.
// @Tags			Configuração do workspace
// @Produce		json
// @Param			workspaceId	path		string	true	"ID do workspace"
// @Success		200			{object}	GeocodingSettingsResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/geocoding [get]
func (h *WorkspaceConfigHandler) GetGeocodingSettings(w http.ResponseWriter, r *http.Request) {
	editor, ok := h.geocodingEditor(w, r)
	if !ok {
		return
	}
	view, err := h.geocoding.Get(r.Context(), mux.Vars(r)["workspaceId"], editor)
	if err != nil {
		writeGeocodingError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toGeocodingSettingsResponse(view))
}

// @Summary		Escolher o provedor externo de geolocalização e o teto mensal
// @Description	`provider` liga (`"opencage"`) ou desliga (`""`) o provedor externo para o workspace. Só o dono do workspace ou um administrador da plataforma pode mudar (403 `geocoding_forbidden`), e quem mudou e quando ficam registrados. Ligar envia endereços dos leads a um suboperador (OpenCage, que é instruído a não guardar as consultas). Um provedor que este servidor não tem configurado responde 409 `geocoding_provider_not_configured`; um nome desconhecido responde 400 `geocoding_provider_unknown`. `monthlyCeiling` muda o teto mensal de consultas externas (0 a 1.000.000; 0 desliga as consultas) e só um administrador da plataforma pode mudar (403 `geocoding_ceiling_forbidden`; fora da faixa, 400 `geocoding_ceiling_invalid`). Um campo ausente fica como está; ao menos um é obrigatório (400 `geocoding_change_empty`). Uma chave desconhecida ou um tipo errado responde 400 `invalid_body`. Responde a configuração como o GET.
// @Tags			Configuração do workspace
// @Accept			json
// @Produce		json
// @Param			workspaceId	path		string							true	"ID do workspace"
// @Param			request		body		UpdateGeocodingSettingsRequest	true	"Campos a mudar"
// @Success		200			{object}	GeocodingSettingsResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		409			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Failure		503			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workspaces/{workspaceId}/geocoding [put]
func (h *WorkspaceConfigHandler) UpdateGeocodingSettings(w http.ResponseWriter, r *http.Request) {
	editor, ok := h.geocodingEditor(w, r)
	if !ok {
		return
	}
	var req UpdateGeocodingSettingsRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	change := geocoding.Change{MonthlyCeiling: req.MonthlyCeiling}
	if req.Provider != nil {
		provider := geocoding.Provider(strings.TrimSpace(*req.Provider))
		change.Provider = &provider
	}
	view, err := h.geocoding.Change(r.Context(), mux.Vars(r)["workspaceId"], editor, change)
	if err != nil {
		writeGeocodingError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toGeocodingSettingsResponse(view))
}

func (h *WorkspaceConfigHandler) geocodingEditor(w http.ResponseWriter, r *http.Request) (wsc.Caller, bool) {
	editor, ok := sessionCaller(w, r)
	if !ok {
		return wsc.Caller{}, false
	}
	if h.geocoding == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "geocoding_unavailable", "A geolocalização de endereços não está disponível neste servidor", nil)
		return wsc.Caller{}, false
	}
	return editor, true
}

func writeGeocodingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace_config_usecase.ErrNotWorkspaceMember):
		response.WriteErrorWithCode(w, http.StatusForbidden, "geocoding_forbidden", "Você não é membro deste workspace", nil)
	case errors.Is(err, geocoding.ErrSettingsForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, "geocoding_forbidden", "Só o dono do workspace ou um administrador da plataforma pode escolher o provedor externo", nil)
	case errors.Is(err, geocoding.ErrCeilingForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, "geocoding_ceiling_forbidden", "Só um administrador da plataforma pode mudar o teto mensal", nil)
	case errors.Is(err, geocoding.ErrChangeEmpty):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "geocoding_change_empty", err.Error(), nil)
	case errors.Is(err, geocoding.ErrProviderUnknown):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "geocoding_provider_unknown", err.Error(), nil)
	case errors.Is(err, geocoding.ErrCeilingInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "geocoding_ceiling_invalid", err.Error(), nil)
	case errors.Is(err, geocoding.ErrProviderNotConfigured):
		response.WriteErrorWithCode(w, http.StatusConflict, "geocoding_provider_not_configured", "Este servidor não tem acesso a esse provedor", nil)
	default:
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "geocoding_settings_unreadable", "A configuração de geolocalização não pôde ser lida", nil)
	}
}

func toGeocodingSettingsResponse(view workspace_config_usecase.GeocodingSettingsView) GeocodingSettingsResponse {
	s := view.Settings
	out := GeocodingSettingsResponse{
		Provider:              string(s.Provider),
		Enabled:               s.Enabled(),
		ProviderChangedBy:     s.ProviderChangedBy,
		ProviderChangedByName: view.ProviderChangedByName,
		ProviderChangedAt:     rfc3339(s.ProviderChangedAt),
		MonthlyCeiling:        s.Ceiling(),
		CeilingChangedBy:      s.CeilingChangedBy,
		CeilingChangedByName:  view.CeilingChangedByName,
		CeilingChangedAt:      rfc3339(s.CeilingChangedAt),
		UsedThisCycle:         view.Usage.Requests,
		UsedToday:             view.Usage.DayRequests,
		AvailableProviders:    make([]string, 0, len(view.Providers)),
		Attribution:           georef.Attribution,
		CanChangeProvider:     view.CanChangeProvider,
		CanChangeCeiling:      view.CanChangeCeiling,
	}
	for _, p := range view.Providers {
		out.AvailableProviders = append(out.AvailableProviders, string(p))
	}
	if view.Slot != nil {
		out.DailyShare = view.Slot.DailyLimit
		out.CycleStart, out.NextCycleStart = rfc3339(&view.Slot.CycleStart), rfc3339(&view.Slot.NextCycle)
		exhausted, _ := view.Slot.Refusal(view.Usage)
		out.Exhausted = string(exhausted)
	}
	out.ProviderPause = providerPauseResponse(view)
	return out
}

func providerPauseResponse(view workspace_config_usecase.GeocodingSettingsView) *GeocodingProviderPauseResponse {
	switch {
	case view.PauseUnreadable:
		return &GeocodingProviderPauseResponse{State: "unknown"}
	case view.Pause != nil:
		return &GeocodingProviderPauseResponse{State: "paused", Reason: string(view.Pause.Reason), Since: rfc3339(&view.Pause.Since), Until: rfc3339(&view.Pause.Until)}
	}
	return nil
}
