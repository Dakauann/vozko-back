package httpx

import (
	"net/http"
	"strconv"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
)

const (
	CodeVersionRequired = "version_required"
	CodeVersionConflict = "version_conflict"
)

type VersionConflict struct {
	Code    string `json:"code" example:"version_conflict"`
	Message string `json:"message"`
	Current any    `json:"current" swaggertype:"object"`
}

func IfMatchVersion(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\"")
	version, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || shared.RequireVersion(version) != nil {
		WriteVersionRequired(w)
		return 0, false
	}
	return version, true
}

func WriteVersionRequired(w http.ResponseWriter) {
	response.WriteErrorWithCode(w, http.StatusPreconditionRequired, CodeVersionRequired, "Informe a versão carregada no cabeçalho If-Match", nil)
}

func WriteVersionConflict(w http.ResponseWriter, message string, current any) {
	response.WriteSuccess(w, http.StatusConflict, VersionConflict{Code: CodeVersionConflict, Message: message, Current: current})
}
