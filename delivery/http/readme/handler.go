package readme

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/delivery/http/response"
	"vozko/domain/auth"
	userdomain "vozko/domain/user"
)

const (
	maxBodyBytes       = 64 << 10
	maxSignatureAge    = 30 * time.Minute
	maxFutureClockSkew = 5 * time.Minute
)

type UserFinder interface {
	FindByEmail(email string) (*userdomain.User, error)
}

type TokenIssuer interface {
	Issue(user *userdomain.User) (*auth.TokenPair, error)
}

type Handler struct {
	secret string
	users  UserFinder
	tokens TokenIssuer
	now    func() time.Time
}

type webhookRequest struct {
	Email string `json:"email"`
}

type webhookResponse struct {
	Authorization string `json:"Authorization"`
}

func NewHandler(secret string, users UserFinder, tokens TokenIssuer) *Handler {
	return &Handler{
		secret: strings.TrimSpace(secret),
		users:  users,
		tokens: tokens,
		now:    time.Now,
	}
}

func (h *Handler) PersonalizedDocs(w http.ResponseWriter, r *http.Request) {
	if h.secret == "" || h.users == nil || h.tokens == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "personalized docs webhook is not configured", nil)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "failed to read webhook payload", nil)
		return
	}

	var payload webhookRequest
	if err := json.Unmarshal(body, &payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"email": "string (required)"})
		return
	}
	canonicalBody, err := json.Marshal(payload)
	if err != nil || !verifySignature(canonicalBody, r.Header.Get("ReadMe-Signature"), h.secret, h.now()) {
		response.WriteError(w, http.StatusUnauthorized, "invalid webhook signature", nil)
		return
	}

	payload.Email = strings.TrimSpace(payload.Email)
	if payload.Email == "" {
		response.WriteValidationError(w, map[string]string{"email": "required"})
		return
	}
	user, err := h.users.FindByEmail(payload.Email)
	if err != nil {
		if errors.Is(err, userdomain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "user not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "failed to load user", nil)
		return
	}

	tokens, err := h.tokens.Issue(user)
	if err != nil || tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		response.WriteError(w, http.StatusInternalServerError, "failed to authorize personalized docs", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, webhookResponse{Authorization: "Bearer " + tokens.AccessToken})
}

func verifySignature(body []byte, signature, secret string, now time.Time) bool {
	var timestamp int64
	var providedSignature string
	for _, part := range strings.Split(signature, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return false
			}
			timestamp = parsed
		case "v0":
			providedSignature = value
		}
	}
	if timestamp <= 0 || providedSignature == "" {
		return false
	}
	age := now.Sub(time.UnixMilli(timestamp))
	if age > maxSignatureAge || age < -maxFutureClockSkew {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp)
	_, _ = mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expectedSignature), []byte(providedSignature))
}
