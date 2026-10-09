package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
)

const CodeInvalidBody = "invalid_body"

func RequireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", false
	}
	return workspaceID, true
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return false
	}
	return true
}

func DecodeStrictJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, CodeInvalidBody, strictBodyRefusal(err), nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, CodeInvalidBody, "invalid request body: send a single JSON object", nil)
		return false
	}
	return true
}

const maxOptionalBodyBytes = 16 << 10

func DecodeOptionalStrictJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxOptionalBodyBytes+1))
	if err != nil || len(raw) > maxOptionalBodyBytes {
		response.WriteErrorWithCode(w, http.StatusBadRequest, CodeInvalidBody, "invalid request body", nil)
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return DecodeStrictJSON(w, r, into)
}

func strictBodyRefusal(err error) string {
	if field, unknown := strings.CutPrefix(err.Error(), "json: unknown field "); unknown {
		return "invalid request body: this route does not take the field " + field
	}
	return "invalid request body"
}
