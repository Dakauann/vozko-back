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

// Notification is the envelope Mercado Pago POSTs to the webhook URL.
//
// It deliberately carries no payment state: only a resource id under data.id. Acting
// on it therefore always requires a GET /v1/payments/{id}, which is the single largest
// behavioural difference from Asaas (whose webhook ships the whole payment).
//
// Reference: https://www.mercadopago.com/developers/en/docs/your-integrations/notifications/webhooks
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
	// Resource and legacy topic fields are only populated by the older IPN format,
	// where the resource id arrives as a URL rather than an id.
	Resource string `json:"resource"`
}

// Notification types. Only payment notifications move money in this integration; the
// rest are acknowledged and ignored so Mercado Pago stops retrying them.
const (
	NotificationTypePayment = "payment"
)

// ResourceID returns the payment id a notification refers to, normalizing across the
// two envelope formats Mercado Pago still emits: the modern one puts it in data.id,
// while the legacy IPN puts a full resource URL in "resource".
func (n *Notification) ResourceID() string {
	if n == nil {
		return ""
	}
	if id := strings.TrimSpace(n.Data.ID); id != "" {
		return id
	}
	if res := strings.TrimSpace(n.Resource); res != "" {
		// e.g. https://api.mercadolibre.com/collections/notifications/123456
		if idx := strings.LastIndex(res, "/"); idx >= 0 && idx+1 < len(res) {
			return res[idx+1:]
		}
		return res
	}
	return ""
}

// NormalizedType returns the notification family, tolerating the legacy "topic" field
// that older Mercado Pago webhook configurations still send instead of "type".
func (n *Notification) NormalizedType() string {
	if n == nil {
		return ""
	}
	if t := strings.ToLower(strings.TrimSpace(n.Type)); t != "" {
		return t
	}
	return strings.ToLower(strings.TrimSpace(n.Topic))
}

// IsPayment reports whether this notification concerns a payment resource.
func (n *Notification) IsPayment() bool {
	return n.NormalizedType() == NotificationTypePayment
}

// ParseNotification decodes a webhook body, falling back to the query string for the
// legacy IPN format, which sends "?topic=payment&id=123" with no body at all.
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

// ErrNotificationMissingResourceID means neither the body nor the query string carried
// an id, so there is nothing to fetch. Retrying cannot help.
var ErrNotificationMissingResourceID = errors.New("mercadopago: notification has no resource id")

// SignatureReason describes why signature verification failed. It is logged rather
// than returned to the caller, since a webhook sender must never learn which part of
// its forgery was wrong.
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

// SignatureError is returned by VerifySignature on any rejection.
type SignatureError struct {
	Reason    SignatureReason
	RequestID string
	Timestamp string
}

func (e *SignatureError) Error() string {
	return "mercadopago: invalid webhook signature: " + string(e.Reason)
}

// ErrInvalidSignature is the sentinel every SignatureError satisfies, so callers can
// reject with errors.Is without enumerating reasons.
var ErrInvalidSignature = errors.New("mercadopago: invalid webhook signature")

// Is lets errors.Is(err, ErrInvalidSignature) match any SignatureError.
func (e *SignatureError) Is(target error) bool { return target == ErrInvalidSignature }

var signatureVersionKey = regexp.MustCompile(`^v\d+$`)

// VerifySignature validates the x-signature header Mercado Pago sends with every
// webhook.
//
// The signed manifest is "id:<data.id>;request-id:<x-request-id>;ts:<ts>;" with any
// empty component omitted entirely, HMAC-SHA256 over the application's secret, hex
// encoded, compared in constant time.
//
// Two details are easy to get wrong and are handled explicitly here:
//
//   - data.id comes from the QUERY STRING, not the JSON body. The two agree in
//     practice, but Mercado Pago signs the query parameter, so that is what is used.
//   - The documentation says an alphanumeric id must be lowercased before hashing,
//     while Mercado Pago's own SDK hashes it verbatim. Payment ids are numeric so the
//     two agree today; both candidates are tried so a future non-numeric id cannot
//     silently start rejecting every webhook.
//
// tolerance <= 0 disables the replay window. That is the default on purpose: Mercado
// Pago retries a failed delivery for hours WITHOUT re-signing, so a tight window would
// reject exactly the retries that matter most.
//
// Reference: https://www.mercadopago.com/developers/en/docs/your-integrations/notifications/webhooks
func VerifySignature(xSignature, xRequestID, dataID, secret string, tolerance time.Duration, now time.Time) error {
	_, err := VerifySignatureAny(xSignature, xRequestID, []string{dataID}, secret, tolerance, now)
	return err
}

// VerifySignatureAny verifies the signature against several candidate resource ids and
// reports which one matched.
//
// Mercado Pago is not consistent about where the signed id comes from. The documented
// source is the data.id QUERY parameter, and that is what its own SDK reads, but some
// notifications arrive with no query parameter at all while still being signed over the
// id carried in the body. Verifying against one source alone therefore rejects a subset
// of perfectly authentic notifications — silently losing whichever payments happen to
// arrive in that shape.
//
// Trying several candidates is not a weakening of the check. Each candidate is a full
// HMAC comparison against the same secret, so an attacker who cannot produce a valid
// HMAC still cannot pass, no matter how many manifests are tried. What it buys is the
// matched id: the caller can then act on the id that was actually signed, instead of
// verifying one value and processing another.
func VerifySignatureAny(xSignature, xRequestID string, dataIDs []string, secret string, tolerance time.Duration, now time.Time) (string, error) {
	xSignature = strings.TrimSpace(xSignature)
	xRequestID = strings.TrimSpace(xRequestID)
	secret = strings.TrimSpace(secret)

	if secret == "" {
		// Fail closed. An unconfigured secret must never turn the endpoint into an
		// open one that anyone can use to mark invoices paid.
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

	// Candidate manifests, in the order Mercado Pago is most likely to have signed:
	// each supplied id lowercased (what the documentation prescribes) and verbatim
	// (what its own SDK actually hashes). Duplicates are skipped so the common case
	// costs a single HMAC.
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

// parseSignatureHeader splits "ts=...,v1=..." into its parts, ignoring unknown keys so
// a future added component cannot break verification.
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

// buildManifest assembles the signed string, dropping empty components entirely. The
// trailing semicolon is part of the format.
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
