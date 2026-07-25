package response

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Error    bool              `json:"error"`
	Code     string            `json:"code,omitempty"`
	Message  string            `json:"message"`
	Expected map[string]string `json:"expected,omitempty"`
	Optional []string          `json:"optional,omitempty"`
}

type CodedErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func WriteCodedError(w http.ResponseWriter, statusCode int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(CodedErrorResponse{Error: code, Message: message})
}

type PaginationMeta struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalPages int   `json:"totalPages"`
	TotalItems int64 `json:"totalItems"`
}

type PaginatedPayload struct {
	Data interface{}    `json:"data"`
	Meta PaginationMeta `json:"meta"`
}

func WriteError(w http.ResponseWriter, statusCode int, message string, expected map[string]string, optional ...string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	payload := ErrorResponse{
		Error:    true,
		Message:  message,
		Expected: expected,
	}

	if len(optional) > 0 {
		payload.Optional = append([]string(nil), optional...)
	}

	json.NewEncoder(w).Encode(payload)
}

func WriteValidationError(w http.ResponseWriter, expected map[string]string, optional ...string) {
	WriteError(w, http.StatusBadRequest, "Validation failed", expected, optional...)
}

func WriteErrorWithCode(w http.ResponseWriter, statusCode int, code, message string, expected map[string]string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:    true,
		Code:     code,
		Message:  message,
		Expected: expected,
	})
}

func WriteInvalidBodyError(w http.ResponseWriter, expected map[string]string) {
	WriteError(w, http.StatusBadRequest, "Invalid request body", expected)
}

func WriteSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if statusCode != http.StatusOK {
		w.WriteHeader(statusCode)
	}
	json.NewEncoder(w).Encode(data)
}

func WritePaginated(w http.ResponseWriter, statusCode int, data interface{}, meta PaginationMeta) {
	WriteSuccess(w, statusCode, PaginatedPayload{Data: data, Meta: meta})
}
