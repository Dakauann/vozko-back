package mercadopago

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Notification struct {
	ID         json.Number `json:"id"`
	LiveMode   bool        `json:"live_mode"`
	Type       string      `json:"type"`
	Topic      string      `json:"topic"`
	DateeCreat string      `json:"date_created"`
	UserID     json.Number `json:"user_id"`
	APIVersion string      `json:"api_version"`
	Action     string      `json:"action"`
	Data       struct {
		ID string `json:"id"`
	} `json:"data"`
	Resource string `json:"resource"`
}

const (
	NotificationTypePayment = "payment"
)

func (n *Notification) ResourceID() string {
	if n == nil {
		return ""
	}
	if id := strings.TrimSpace(n.Data.ID); id != "" {
		return id
	}
	if res := strings.TrimSpace(n.Resource); res != "" {
		if idx := strings.LastIndex(res, "/"); idx >= 0 && idx+1 < len(res) {
			return res[idx+1:]
		}
		return res
	}
	return ""
}

func (n *Notification) NormalizedType() string {
	if n == nil {
		return ""
	}
	if t := strings.ToLower(strings.TrimSpace(n.Type)); t != "" {
		return t
	}
	return strings.ToLower(strings.TrimSpace(n.Topic))
}

func (n *Notification) IsPayment() bool {
	return n.NormalizedType() == NotificationTypePayment
}

func ParseNotification(body []byte, query url.Values) (*Notification, error) {
	var n Notification
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &n); err != nil {
			return nil, fmt.Errorf("mercadopago: invalid notification body: %w", err)
		}
	}

	if n.ResourceID() == "" && query != nil {
		if id := strings.TrimSpace(query.Get("data.id")); id != "" {
			n.Data.ID = id
		} else if id := strings.TrimSpace(query.Get("id")); id != "" {
			n.Data.ID = id
		}
	}
	if n.NormalizedType() == "" && query != nil {
		if t := strings.TrimSpace(query.Get("type")); t != "" {
			n.Type = t
		} else if t := strings.TrimSpace(query.Get("topic")); t != "" {
			n.Topic = t
		}
	}

	if n.ResourceID() == "" {
		return nil, ErrNotificationMissingResourceID
	}
	return &n, nil
}

var ErrNotificationMissingResourceID = errors.New("mercadopago: notification has no resource id")

type SignatureReason string

const (
	ReasonMissingSecret           SignatureReason = "MissingSecret"
	ReasonMissingSignatureHeader  SignatureReason = "MissingSignatureHeader"
	ReasonMalformedSignature      SignatureReason = "MalformedSignatureHeader"
	ReasonMissingTimestamp        SignatureReason = "MissingTimestamp"
	ReasonMissingHash             SignatureReason = "MissingHash"
	ReasonSignatureMismatch       SignatureReason = "SignatureMismatch"
	ReasonTimestampOutOfTolerance SignatureReason = "TimestampOutOfTolerance"
)

type SignatureError struct {
	Reason    SignatureReason
	RequestID string
	Timestamp string
}

func (e *SignatureError) Error() string {
	return "mercadopago: invalid webhook signature: " + string(e.Reason)
}

var ErrInvalidSignature = errors.New("mercadopago: invalid webhook signature")

func (e *SignatureError) Is(target error) bool { return target == ErrInvalidSignature }

var signatureVersionKey = regexp.MustCompile(`^v\d+$`)

func VerifySignature(xSignature, xRequestID, dataID, secret string, tolerance time.Duration, now time.Time) error {
	_, err := VerifySignatureAny(xSignature, xRequestID, []string{dataID}, secret, tolerance, now)
	return err
}

func VerifySignatureAny(xSignature, xRequestID string, dataIDs []string, secret string, tolerance time.Duration, now time.Time) (string, error) {
	xSignature = strings.TrimSpace(xSignature)
	xRequestID = strings.TrimSpace(xRequestID)
	secret = strings.TrimSpace(secret)

	if secret == "" {
		return "", &SignatureError{Reason: ReasonMissingSecret, RequestID: xRequestID}
	}
	if xSignature == "" {
		return "", &SignatureError{Reason: ReasonMissingSignatureHeader, RequestID: xRequestID}
	}

	ts, hashes := parseSignatureHeader(xSignature)
	if ts == "" && len(hashes) == 0 {
		return "", &SignatureError{Reason: ReasonMalformedSignature, RequestID: xRequestID}
	}
	if ts == "" {
		return "", &SignatureError{Reason: ReasonMissingTimestamp, RequestID: xRequestID}
	}
	tsMillis, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", &SignatureError{Reason: ReasonMalformedSignature, RequestID: xRequestID, Timestamp: ts}
	}

	received := hashes["v1"]
	if received == "" {
		return "", &SignatureError{Reason: ReasonMissingHash, RequestID: xRequestID, Timestamp: ts}
	}

	type candidate struct {
		manifest string
		dataID   string
	}
	var candidates []candidate
	seen := map[string]struct{}{}
	addCandidate := func(id string) {
		manifest := buildManifest(id, xRequestID, ts)
		if _, dup := seen[manifest]; dup {
			return
		}
		seen[manifest] = struct{}{}
		candidates = append(candidates, candidate{manifest: manifest, dataID: id})
	}
	for _, raw := range dataIDs {
		id := strings.TrimSpace(raw)
		addCandidate(strings.ToLower(id))
		addCandidate(id)
	}
	if len(candidates) == 0 {
		addCandidate("")
	}

	matchedID := ""
	matched := false
	for _, c := range candidates {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(c.manifest))
		computed := hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(computed), []byte(received)) {
			matchedID = c.dataID
			matched = true
			break
		}
	}
	if !matched {
		return "", &SignatureError{Reason: ReasonSignatureMismatch, RequestID: xRequestID, Timestamp: ts}
	}

	if tolerance > 0 {
		if now.IsZero() {
			now = time.Now()
		}
		drift := now.UnixMilli() - tsMillis
		if drift < 0 {
			drift = -drift
		}
		if drift > tolerance.Milliseconds() {
			return "", &SignatureError{Reason: ReasonTimestampOutOfTolerance, RequestID: xRequestID, Timestamp: ts}
		}
	}

	return matchedID, nil
}

func parseSignatureHeader(header string) (ts string, hashes map[string]string) {
	hashes = map[string]string{}
	for _, part := range strings.Split(header, ",") {
		rawKey, rawValue, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(rawKey))
		value := strings.TrimSpace(rawValue)
		if key == "" || value == "" {
			continue
		}
		if key == "ts" {
			ts = value
			continue
		}
		if signatureVersionKey.MatchString(key) {
			hashes[key] = value
		}
	}
	return ts, hashes
}

func buildManifest(dataID, requestID, ts string) string {
	parts := make([]string, 0, 3)
	if dataID != "" {
		parts = append(parts, "id:"+dataID)
	}
	if requestID != "" {
		parts = append(parts, "request-id:"+requestID)
	}
	parts = append(parts, "ts:"+ts)
	return strings.Join(parts, ";") + ";"
}
