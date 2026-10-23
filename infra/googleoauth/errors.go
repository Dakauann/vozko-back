package googleoauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"vozko/domain/googleaccount"
)

type APIError struct {
	Status      int
	Code        string
	Description string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("googleoauth: google answered %d %s %s", e.Status, e.Code, e.Description)
}

func newAPIError(status int, raw []byte) *APIError {
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &body)
	return &APIError{Status: status, Code: body.Error, Description: body.Description}
}

type Classifier struct{}

var _ googleaccount.ErrorClassifier = Classifier{}

func (Classifier) IsAuth(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == "invalid_grant" || apiErr.Code == "unauthorized_client" || apiErr.Status == http.StatusUnauthorized
}
