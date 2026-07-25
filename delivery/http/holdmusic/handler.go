package holdmusic

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
)

type Track struct {
	Key   string
	Label string
}

type Catalog interface {
	Tracks() []Track
	Path(key string) (string, bool)
}

type HoldMusicHandler struct {
	catalog Catalog
}

func NewHoldMusicHandler(catalog Catalog) *HoldMusicHandler {
	return &HoldMusicHandler{catalog: catalog}
}

// @Summary		Listar músicas de espera padrão
// @Description	Retorna o catálogo de faixas de música de espera já incluídas no servidor, disponíveis para seleção nas configurações da conta.
// @Tags			Música de espera
// @Produce		json
// @Success		200	{object}	HoldTrackListResponse
// @Security		BearerAuth
// @Router			/hold-music/builtins [get]
func (h *HoldMusicHandler) ListBuiltins(w http.ResponseWriter, r *http.Request) {
	tracks := h.catalog.Tracks()
	items := make([]HoldTrackResponse, len(tracks))
	for i, t := range tracks {
		items[i] = HoldTrackResponse{Key: t.Key, Label: t.Label}
	}
	response.WriteSuccess(w, http.StatusOK, HoldTrackListResponse{Tracks: items})
}

// @Summary		Reproduzir uma música de espera padrão
// @Description	Transmite o áudio de uma faixa padrão para pré-visualização no seletor de configurações. A busca no catálogo é a proteção contra travessia de caminho: apenas chaves conhecidas resolvem para arquivos.
// @Tags			Música de espera
// @Produce		audio/mpeg
// @Param			key	path	string	true	"Chave da faixa (ex.: bossa_nova)"
// @Success		200	{file}	binary	"Áudio MP3 da faixa"
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/hold-music/builtins/{key}/audio [get]
func (h *HoldMusicHandler) StreamBuiltin(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	path, ok := h.catalog.Path(key)
	if !ok {
		response.WriteError(w, http.StatusNotFound, "Unknown hold music track", nil)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}
