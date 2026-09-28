package metaplatform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	mp "vozko/domain/metaplatform"
	mpuc "vozko/usecases/metaplatform"
)

type memoryRequests struct{ rows map[string]*mp.DeletionRequest }

func (m *memoryRequests) Create(_ context.Context, r *mp.DeletionRequest) error {
	stored := *r
	m.rows[r.Code] = &stored
	return nil
}

func (m *memoryRequests) FindByCode(_ context.Context, code string) (*mp.DeletionRequest, error) {
	if r, ok := m.rows[code]; ok {
		return r, nil
	}
	return nil, mp.ErrRequestNotFound
}

func (m *memoryRequests) Finish(_ context.Context, code string, status mp.DeletionStatus, detail string, at time.Time) error {
	r := m.rows[code]
	r.Status, r.CompletedAt = status, &at
	return nil
}

type recordingHandler struct{ revoked, erased []string }

func (h *recordingHandler) RevokeAppUser(_ context.Context, id string) error {
	h.revoked = append(h.revoked, id)
	return nil
}

func (h *recordingHandler) EraseAppUser(_ context.Context, id string) error {
	h.erased = append(h.erased, id)
	return nil
}

func signed(secret, userID string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"algorithm":"HMAC-SHA256","issued_at":1,"user_id":"` + userID + `"}`))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + payload
}

func setup() (*mux.Router, *recordingHandler, *recordingHandler) {
	svc := mpuc.NewService(&memoryRequests{rows: map[string]*mp.DeletionRequest{}}, "https://app.example")
	metaHandler, igHandler := &recordingHandler{}, &recordingHandler{}
	svc.Register(mp.AppMeta, metaHandler)
	svc.Register(mp.AppInstagram, igHandler)
	h := NewHandler(svc, map[mp.App][]string{
		mp.AppMeta:      {"meta-old", "meta-secret"},
		mp.AppInstagram: {"ig-secret"},
	})
	r := mux.NewRouter()
	RegisterPublicRoutes(r, h)
	return r, metaHandler, igHandler
}

func postForm(r http.Handler, path, signedRequest string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"signed_request": {signedRequest}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDeauthorizeVerifiesWithAnyConfiguredSecret(t *testing.T) {
	r, metaHandler, igHandler := setup()
	rec := postForm(r, "/webhooks/meta/deauthorize", signed("meta-secret", "asid-1"))
	if rec.Code != http.StatusOK || len(metaHandler.revoked) != 1 || len(igHandler.revoked) != 0 {
		t.Fatalf("code=%d meta=%v ig=%v", rec.Code, metaHandler.revoked, igHandler.revoked)
	}
}

func TestDeauthorizeRejectsForeignSignature(t *testing.T) {
	r, metaHandler, _ := setup()
	rec := postForm(r, "/webhooks/meta/deauthorize", signed("ig-secret", "asid-1"))
	if rec.Code != http.StatusBadRequest || len(metaHandler.revoked) != 0 {
		t.Fatalf("code=%d revoked=%v", rec.Code, metaHandler.revoked)
	}
}

func TestDataDeletionAnswersWithURLAndCode(t *testing.T) {
	r, _, igHandler := setup()
	rec := postForm(r, "/webhooks/instagram/data-deletion", signed("ig-secret", "ig-user"))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		URL              string `json:"url"`
		ConfirmationCode string `json:"confirmation_code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ConfirmationCode == "" || body.URL != "https://app.example/data-deletion?code="+body.ConfirmationCode {
		t.Fatalf("body = %+v", body)
	}
	if len(igHandler.erased) != 1 {
		t.Fatalf("erased = %v", igHandler.erased)
	}

	status := httptest.NewRecorder()
	r.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/meta/data-deletion/"+body.ConfirmationCode, nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"COMPLETED"`) {
		t.Fatalf("status lookup = %d %s", status.Code, status.Body.String())
	}
}

func TestDeletionStatusUnknownCode(t *testing.T) {
	r, _, _ := setup()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/meta/data-deletion/unknown", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestMissingSignedRequestIsRejected(t *testing.T) {
	r, _, _ := setup()
	rec := postForm(r, "/webhooks/meta/data-deletion", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}
