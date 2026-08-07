// Package uazapi is the uazapi transport for the unofficial WhatsApp channel.
//
// It is deliberately the only place in the codebase that knows this vendor
// exists. Everything above it works with domain/unofficial_whatsapp's
// contracts, which is what makes a second provider — Evolution API, WPPConnect,
// a self-hosted Baileys — another implementation of the same port rather than a
// second entry type and a second pass over every CRM registry.
//
// Two vendor facts shape this file:
//
//  1. Credentials are plain headers with no expiry: `admintoken` is host-wide,
//     `token` is per instance. Both are passed per call rather than held as
//     client state, because a workspace can connect several numbers across
//     several hosts.
//  2. Failures are not uniform. The host's own errors and WhatsApp's forwarded
//     refusals arrive through the same HTTP status, and only the body tells
//     them apart. Conflating them is how a WhatsApp warning gets retried into a
//     ban, so decodeError below is the load-bearing function here.
package uazapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// requestTimeout bounds a single call. Generous because instance provisioning
// and connect are slow on the host, but finite: a hung call holds a slot in the
// caller's rate budget for its whole duration.
const requestTimeout = 45 * time.Second

// maxResponseBytes caps what we will read from the host. A media download can be
// large, but an unbounded read of a remote body is an availability bug waiting
// for a bad day.
const maxResponseBytes = 64 << 20

// Header names. Vendor-specific, hence private to this package.
const (
	headerInstanceToken = "token"
	headerAdminToken    = "admintoken"
)

// Config configures the client.
type Config struct {
	HTTPClient *http.Client
}

// Client implements the provider ports in domain/unofficial_whatsapp.
//
// It holds no base URL and no credential: both travel with each call, in the
// ServerRef or InstanceRef the domain defines.
type Client struct {
	http *http.Client
}

// NewClient builds the provider client.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{http: httpClient}
}

// Compile-time proof that the client satisfies the whole provider surface. If a
// port method is added, this fails here rather than at wiring time.
var _ uw.ProviderAPI = (*Client)(nil)

// ---------------------------------------------------------------- transport

// call issues one request and decodes the response body into out.
//
// baseURL and credential are explicit for the reason in the package comment.
// A nil body sends no payload; a nil out discards the response.
func (c *Client) call(
	ctx context.Context,
	baseURL, credHeader, credential, method, path string,
	body any,
	out any,
) error {
	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + path

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("uazapi: encode %s: %w", path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("uazapi: build %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if credential != "" {
		req.Header.Set(credHeader, credential)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("uazapi: %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("uazapi: read %s: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp.StatusCode, raw)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("uazapi: decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) instanceCall(ctx context.Context, ref uw.InstanceRef, method, path string, body, out any) error {
	return c.call(ctx, ref.BaseURL, headerInstanceToken, ref.Token, method, path, body, out)
}

func (c *Client) adminCall(ctx context.Context, server uw.ServerRef, method, path string, body, out any) error {
	return c.call(ctx, server.BaseURL, headerAdminToken, server.AdminToken, method, path, body, out)
}

// ---------------------------------------------------------------- errors

// errorBody is the union of every failure shape the host emits.
//
// The vendor answers a plain host error with `{error}` and a forwarded WhatsApp
// refusal with a much richer object. Decoding both here, rather than at each
// call site, is what lets ProviderError.IsRestriction be trustworthy.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`

	ErrorSource  string `json:"error_source"`
	Provider     string `json:"provider"`
	ProviderCode int    `json:"provider_code"`
	ErrorKey     string `json:"error_key"`

	MessagePtBR         string `json:"message_ptbr"`
	ProviderMessage     string `json:"provider_message"`
	ProviderMessagePtBR string `json:"provider_message_ptbr"`

	Details struct {
		NewChatMessageCapping *chatCapping `json:"new_chat_message_capping"`
		ReachoutTimelock      *timelock    `json:"reachout_timelock"`
	} `json:"details"`
}

type chatCapping struct {
	Available  bool   `json:"available"`
	Status     string `json:"status"`
	UsedQuota  int    `json:"used_quota"`
	TotalQuota int    `json:"total_quota"`
	CycleEnd   string `json:"cycle_end"`
}

type timelock struct {
	Available       bool   `json:"available"`
	Active          bool   `json:"active"`
	Until           string `json:"until"`
	EnforcementType string `json:"enforcement_type"`
}

// decodeError turns a non-2xx response into a structured domain error.
//
// A body that does not parse still yields a ProviderError with the status, so a
// caller can always classify: returning a bare "unexpected response" would make
// a 401 indistinguishable from a 503 and break both the reconnect and the
// capacity paths.
func decodeError(status int, raw []byte) error {
	provErr := &uw.ProviderError{HTTPStatus: status}

	var body errorBody
	if err := json.Unmarshal(raw, &body); err != nil {
		provErr.Message = strings.TrimSpace(truncate(string(raw), 300))
		if provErr.Message == "" {
			provErr.Message = fmt.Sprintf("http %d", status)
		}
		return provErr
	}

	provErr.Message = firstNonEmpty(body.Error, body.Message, body.ProviderMessage, fmt.Sprintf("http %d", status))
	provErr.ErrorSource = body.ErrorSource
	provErr.ProviderCode = body.ProviderCode
	provErr.ErrorKey = body.ErrorKey
	provErr.LocalizedMessage = firstNonEmpty(body.ProviderMessagePtBR, body.MessagePtBR)

	if r := restrictionFromDetails(body); r != nil {
		provErr.Restriction = r
	}
	return provErr
}

// restrictionFromDetails builds the cached restriction state from an error body.
// Returns nil when WhatsApp reported no limit, so a plain host failure does not
// masquerade as one and pause a broadcast for the wrong reason.
func restrictionFromDetails(body errorBody) *uw.Restriction {
	capping, lock := body.Details.NewChatMessageCapping, body.Details.ReachoutTimelock
	if capping == nil && lock == nil {
		return nil
	}

	now := time.Now().UTC()
	blocked := false
	r := &uw.Restriction{
		Key:       body.ErrorKey,
		Message:   firstNonEmpty(body.ProviderMessagePtBR, body.ProviderMessage, body.MessagePtBR),
		CheckedAt: &now,
	}

	if capping != nil && capping.Available {
		r.UsedQuota, r.TotalQuota = capping.UsedQuota, capping.TotalQuota
		if capping.TotalQuota > 0 && capping.UsedQuota >= capping.TotalQuota {
			blocked = true
		}
		if until := parseTime(capping.CycleEnd); until != nil {
			r.Until = until
		}
	}
	if lock != nil && lock.Available && lock.Active {
		blocked = true
		if until := parseTime(lock.Until); until != nil {
			r.Until = until
		}
	}

	canSend := !blocked
	r.CanSendNewChats = &canSend
	return r
}

// ---------------------------------------------------------------- lifecycle

type createInstanceRequest struct {
	Name string `json:"name"`
	// The host's admin-only metadata slots: readable by the instance owner but
	// writable only with the admin token. They are the trace that lets an
	// orphaned instance on a host be matched back to a tenant.
	AdminField01 string `json:"adminField01,omitempty"`
	AdminField02 string `json:"adminField02,omitempty"`
}

type createInstanceResponse struct {
	Instance instanceBody `json:"instance"`
	Token    string       `json:"token"`
	Name     string       `json:"name"`
}

// instanceBody is the host's instance object, narrowed to what we persist.
// Everything omitted here is either the host's own chatbot config (which we
// switch off, see DisableBuiltInChatbot) or fields with no CRM meaning.
type instanceBody struct {
	ID            string `json:"id"`
	Token         string `json:"token"`
	Status        string `json:"status"`
	Name          string `json:"name"`
	PairCode      string `json:"paircode"`
	QRCode        string `json:"qrcode"`
	ProfileName   string `json:"profileName"`
	ProfilePicURL string `json:"profilePicUrl"`
	IsBusiness    bool   `json:"isBusiness"`
	// The vendor spells this field "plataform".
	Platform             string `json:"plataform"`
	Owner                string `json:"owner"`
	LastDisconnect       string `json:"lastDisconnect"`
	LastDisconnectReason string `json:"lastDisconnectReason"`
	AdminField01         string `json:"adminField01"`
	AdminField02         string `json:"adminField02"`
}

func (c *Client) CreateInstance(
	ctx context.Context,
	server uw.ServerRef,
	in uw.CreateInstanceInput,
) (*uw.CreatedInstance, error) {
	var resp createInstanceResponse
	err := c.adminCall(ctx, server, http.MethodPost, "/instance/create", createInstanceRequest{
		Name:         in.Name,
		AdminField01: in.WorkspaceID,
		AdminField02: in.OurInstanceID,
	}, &resp)
	if err != nil {
		return nil, err
	}

	// The host returns the token at the top level and the id inside the
	// instance object; tolerate either placement rather than assuming one, since
	// a missing token silently produces an instance we can never address again.
	token := firstNonEmpty(resp.Token, resp.Instance.Token)
	if token == "" || resp.Instance.ID == "" {
		return nil, &uw.ProviderError{
			HTTPStatus: http.StatusBadGateway,
			Message:    "instance created without an id or token; it cannot be addressed and must be recreated",
		}
	}
	return &uw.CreatedInstance{
		ProviderInstanceID: resp.Instance.ID,
		Token:              token,
		Name:               firstNonEmpty(resp.Name, resp.Instance.Name, in.Name),
	}, nil
}

func (c *Client) ListInstances(ctx context.Context, server uw.ServerRef) ([]uw.RemoteInstance, error) {
	var raw []instanceBody
	if err := c.adminCall(ctx, server, http.MethodGet, "/instance/all", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]uw.RemoteInstance, 0, len(raw))
	for _, item := range raw {
		out = append(out, uw.RemoteInstance{
			ProviderInstanceID: item.ID,
			Name:               item.Name,
			State:              item.Status,
			WorkspaceID:        item.AdminField01,
			OurInstanceID:      item.AdminField02,
		})
	}
	return out, nil
}

type connectRequest struct {
	// Phone present ⇒ pairing code; absent ⇒ QR code. That is the whole mode
	// switch on the wire, which is why ConnectInput.Mode exists in the domain:
	// an empty phone in pairing mode would silently fall back to a QR the
	// customer is not looking at.
	Phone      string `json:"phone,omitempty"`
	SystemName string `json:"systemName,omitempty"`
}

type sessionResponse struct {
	Instance  instanceBody `json:"instance"`
	Connected bool         `json:"connected"`
	LoggedIn  bool         `json:"loggedIn"`
	JID       any          `json:"jid"`
	Status    *struct {
		Connected bool `json:"connected"`
		LoggedIn  bool `json:"loggedIn"`
		JID       any  `json:"jid"`
	} `json:"status"`
}

// session normalizes the two response shapes the host uses: /instance/connect
// answers with the flags at the top level, /instance/status nests them under
// "status".
func (r sessionResponse) session() *uw.Session {
	connected, loggedIn, jid := r.Connected, r.LoggedIn, r.JID
	if r.Status != nil {
		connected, loggedIn, jid = r.Status.Connected, r.Status.LoggedIn, r.Status.JID
	}

	return &uw.Session{
		State:                r.Instance.Status,
		Connected:            connected,
		LoggedIn:             loggedIn,
		QRCode:               r.Instance.QRCode,
		PairCode:             r.Instance.PairCode,
		JID:                  jidString(jid),
		ProfileName:          r.Instance.ProfileName,
		ProfilePicURL:        r.Instance.ProfilePicURL,
		IsBusiness:           r.Instance.IsBusiness,
		Platform:             r.Instance.Platform,
		LastDisconnectAt:     parseTime(r.Instance.LastDisconnect),
		LastDisconnectReason: r.Instance.LastDisconnectReason,
	}
}

func (c *Client) Connect(ctx context.Context, ref uw.InstanceRef, in uw.ConnectInput) (*uw.Session, error) {
	req := connectRequest{SystemName: in.SystemName}
	if in.Mode == uw.ConnectModePairing {
		req.Phone = uw.NormalizePhone(in.Phone)
		if req.Phone == "" {
			return nil, uw.ErrPhoneRequired
		}
	}

	var resp sessionResponse
	if err := c.instanceCall(ctx, ref, http.MethodPost, "/instance/connect", req, &resp); err != nil {
		return nil, err
	}
	return resp.session(), nil
}

func (c *Client) Status(ctx context.Context, ref uw.InstanceRef) (*uw.Session, error) {
	var resp sessionResponse
	if err := c.instanceCall(ctx, ref, http.MethodGet, "/instance/status", nil, &resp); err != nil {
		return nil, err
	}
	return resp.session(), nil
}

func (c *Client) Disconnect(ctx context.Context, ref uw.InstanceRef) error {
	return c.instanceCall(ctx, ref, http.MethodPost, "/instance/disconnect", nil, nil)
}

func (c *Client) Reset(ctx context.Context, ref uw.InstanceRef) error {
	return c.instanceCall(ctx, ref, http.MethodPost, "/instance/reset", nil, nil)
}

func (c *Client) DeleteInstance(ctx context.Context, ref uw.InstanceRef) error {
	return c.instanceCall(ctx, ref, http.MethodDelete, "/instance", nil, nil)
}

// ---------------------------------------------------------------- webhooks

type webhookBody struct {
	ID                  string   `json:"id,omitempty"`
	Enabled             bool     `json:"enabled"`
	URL                 string   `json:"url"`
	Events              []string `json:"events"`
	ExcludeMessages     []string `json:"excludeMessages"`
	AddURLEvents        bool     `json:"addUrlEvents"`
	AddURLTypesMessages bool     `json:"addUrlTypesMessages"`
}

func (c *Client) SetWebhook(ctx context.Context, ref uw.InstanceRef, sub uw.WebhookSubscription) error {
	// No id and no action: the host's documented "simple mode", which manages a
	// single webhook per instance and upserts it. Passing an id would make this
	// call depend on state we would then have to store and keep in step.
	body := webhookBody{
		Enabled: sub.Enabled,
		URL:     sub.URL,
		Events:  sub.Events,
		// Never nil: the host treats a missing array differently from an empty
		// one, and we mean "exclude nothing" explicitly.
		ExcludeMessages: nonNil(sub.ExcludeMessages),
		// Both false deliberately: the event kind is read from the body, which
		// has to be parsed anyway, and a second source of truth for it in the
		// URL is one that can drift.
		AddURLEvents:        false,
		AddURLTypesMessages: false,
	}
	return c.instanceCall(ctx, ref, http.MethodPost, "/webhook", body, nil)
}

func (c *Client) GetWebhooks(ctx context.Context, ref uw.InstanceRef) ([]uw.WebhookSubscription, error) {
	var raw []webhookBody
	if err := c.instanceCall(ctx, ref, http.MethodGet, "/webhook", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]uw.WebhookSubscription, 0, len(raw))
	for _, item := range raw {
		out = append(out, uw.WebhookSubscription{
			URL:             item.URL,
			Enabled:         item.Enabled,
			Events:          item.Events,
			ExcludeMessages: item.ExcludeMessages,
		})
	}
	return out, nil
}

type webhookErrorBody struct {
	Created    string `json:"created"`
	URL        string `json:"url"`
	Event      string `json:"event"`
	StatusCode int    `json:"status_code"`
	Attempts   int    `json:"attempts"`
	Error      string `json:"error"`
}

func (c *Client) WebhookErrors(ctx context.Context, ref uw.InstanceRef) ([]uw.WebhookDeliveryError, error) {
	var raw []webhookErrorBody
	if err := c.instanceCall(ctx, ref, http.MethodGet, "/webhook/errors", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]uw.WebhookDeliveryError, 0, len(raw))
	for _, item := range raw {
		e := uw.WebhookDeliveryError{
			URL:        item.URL,
			Event:      item.Event,
			StatusCode: item.StatusCode,
			Attempts:   item.Attempts,
			Error:      item.Error,
		}
		if at := parseTime(item.Created); at != nil {
			e.At = *at
		}
		out = append(out, e)
	}
	return out, nil
}

// ---------------------------------------------------------------- diagnostics

type limitsResponse struct {
	CanSendNewMessages  *bool  `json:"can_send_new_messages"`
	ErrorKey            string `json:"error_key"`
	MessagePtBR         string `json:"message_ptbr"`
	ProviderMessagePtBR string `json:"provider_message_ptbr"`

	NewChatMessageCapping *chatCapping `json:"new_chat_message_capping"`
	ReachoutTimelock      *timelock    `json:"reachout_timelock"`
}

func (c *Client) MessagingLimits(ctx context.Context, ref uw.InstanceRef) (*uw.Restriction, error) {
	var resp limitsResponse
	if err := c.instanceCall(ctx, ref, http.MethodGet, "/instance/wa_messages_limits", nil, &resp); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	r := &uw.Restriction{
		CanSendNewChats: resp.CanSendNewMessages,
		Key:             resp.ErrorKey,
		Message:         firstNonEmpty(resp.ProviderMessagePtBR, resp.MessagePtBR),
		CheckedAt:       &now,
	}
	if capping := resp.NewChatMessageCapping; capping != nil && capping.Available {
		r.UsedQuota, r.TotalQuota = capping.UsedQuota, capping.TotalQuota
		if until := parseTime(capping.CycleEnd); until != nil {
			r.Until = until
		}
	}
	if lock := resp.ReachoutTimelock; lock != nil && lock.Available && lock.Active {
		if until := parseTime(lock.Until); until != nil {
			r.Until = until
		}
		// An active timelock is a refusal even when the top-level flag is
		// missing, and a missing flag must never read as permission.
		blocked := false
		r.CanSendNewChats = &blocked
	}
	return r, nil
}

// DisableBuiltInChatbot switches off the host's own AI answering.
//
// This is not hygiene. The host ships a chatbot with its own model key on the
// instance row; left on, it answers the same customer our agent is answering,
// and neither knows about the other. Asserted at provisioning and re-asserted by
// the health cron, because a tenant with console access can turn it back on.
func (c *Client) DisableBuiltInChatbot(ctx context.Context, ref uw.InstanceRef) error {
	// The endpoint that carries the chatbot flag is the instance-row update, and
	// it VALIDATES `name` as non-empty even though the name is not what we came
	// to change — omitting it fails the whole call with "Name cannot be empty"
	// and leaves the host's chatbot running. So the current name is read first
	// and sent back unchanged: this must not become an accidental rename.
	var current sessionResponse
	if err := c.instanceCall(ctx, ref, http.MethodGet, "/instance/status", nil, &current); err != nil {
		return err
	}
	name := strings.TrimSpace(current.Instance.Name)
	if name == "" {
		// Renaming to a placeholder would be worse than not disabling the bot:
		// the name is what the operator identifies the number by on the host's
		// own console. Fail instead, and let the caller log it.
		return &uw.ProviderError{
			HTTPStatus: http.StatusBadGateway,
			Message:    "instance has no name to preserve; refusing to rename it while disabling the chatbot",
		}
	}

	body := map[string]any{"name": name, "chatbot_enabled": false}
	return c.instanceCall(ctx, ref, http.MethodPost, "/instance/updateInstanceName", body, nil)
}

// ---------------------------------------------------------------- helpers

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// parseTime accepts the timestamp shapes the host mixes: RFC3339 with and
// without fractional seconds. An unparseable value yields nil rather than the
// zero time, because a zero timestamp downstream reads as "January 1st year 1"
// in the UI.
func parseTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}
	return nil
}

// jidString extracts a JID from a field the host types loosely: it is null when
// logged out, a string when connected, and occasionally an object carrying the
// parts separately.
func jidString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"User", "user"} {
			user, _ := v[key].(string)
			if user == "" {
				continue
			}
			server, _ := v["Server"].(string)
			if server == "" {
				server, _ = v["server"].(string)
			}
			if server == "" {
				server = uw.DomainUser
			}
			return user + "@" + server
		}
	}
	return ""
}

// decodeBase64Payload decodes a media payload, tolerating a data: URI prefix.
//
// The vendor is inconsistent about whether it returns a bare base64 string or a
// full data URI, and feeding the latter to a bare decoder fails on the first
// colon — which would look like "all media is corrupt".
func decodeBase64Payload(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, nil
	}
	if idx := strings.Index(payload, ";base64,"); idx >= 0 {
		payload = payload[idx+len(";base64,"):]
	}
	if decoded, err := base64.StdEncoding.DecodeString(payload); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(payload)
}
