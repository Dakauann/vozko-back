package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/carriermessaging"
	"vozko/infra/cmcom/cmfake"
)

const (
	sourceIP        = "31.169.56.10"
	webhookTimeout  = 10 * time.Second
	maxControlBytes = 64 << 10
)

var errNotFound = errors.New("not found")

type scheduler func(time.Duration, func())

type report struct {
	Status        string    `json:"status"`
	ErrorCode     string    `json:"error_code,omitempty"`
	At            time.Time `json:"at"`
	BackendStatus int       `json:"backend_status"`
	Error         string    `json:"error,omitempty"`
}

type sendRecord struct {
	Reference string    `json:"reference"`
	Channel   string    `json:"channel"`
	From      string    `json:"from"`
	Phone     string    `json:"phone"`
	Text      string    `json:"text"`
	Accepted  bool      `json:"accepted"`
	ErrorCode int       `json:"error_code,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	At        time.Time `json:"at"`
	Buttons   []button  `json:"buttons"`
	Reports   []report  `json:"reports"`
}

type app struct {
	fake        *cmfake.Fake
	webhookBase string
	http        *http.Client
	after       scheduler
	mux         *http.ServeMux

	mu    sync.Mutex
	sends []*sendRecord
}

func newApp(cfg config, after scheduler) (*app, error) {
	a := &app{webhookBase: cfg.webhookBase, http: &http.Client{Timeout: webhookTimeout}, after: after}
	fake, err := cmfake.Open(cmfake.Options{
		ProductToken: cfg.productToken,
		AccountID:    cfg.accountID,
		Outcome:      outcomeFor,
		OnSend:       a.onSend,
		ProfileProgression: []cmfake.ProfileStatus{
			{EntityStatus: "PENDING"},
			{EntityStatus: "CUSTOMER_ACTION_REQUIRED"},
			{EntityStatus: "ACTIVE"},
		},
	})
	if err != nil {
		return nil, err
	}
	a.fake = fake
	a.mux = http.NewServeMux()
	a.mux.HandleFunc("/_fake", only(http.MethodGet, a.page))
	a.mux.HandleFunc("/_fake/sends", only(http.MethodGet, a.listSends))
	a.mux.HandleFunc("/_fake/inbound", only(http.MethodPost, a.inbound))
	a.mux.HandleFunc("/_fake/tap", only(http.MethodPost, a.tap))
	a.mux.Handle("/", fake)
	return a, nil
}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}

func channelOf(m cmfake.SentMessage) string {
	if len(m.AllowedChannels) == 1 && m.AllowedChannels[0] == "SMS" {
		return channelSMS
	}
	return channelRCS
}

func (a *app) onSend(result cmfake.SendResult) {
	m := result.Message
	buttons, err := buttonsOf(m.Raw)
	if err != nil {
		log.Printf("[cmfake] cannot read the buttons of %s: %v", m.Reference, err)
	}
	record := &sendRecord{
		Reference: m.Reference,
		Channel:   channelOf(m),
		From:      m.From,
		Phone:     m.Phone,
		Text:      m.BodyContent,
		Accepted:  result.Accepted,
		ErrorCode: result.ErrorCode,
		Detail:    result.Detail,
		At:        time.Now().UTC(),
		Buttons:   buttons,
		Reports:   []report{},
	}
	a.mu.Lock()
	a.sends = append(a.sends, record)
	a.mu.Unlock()
	log.Printf("[cmfake] %s send %s to %s accepted=%v", record.Channel, record.Reference, record.Phone, record.Accepted)
	if !result.Accepted {
		return
	}
	for _, s := range lifecycle(record.Channel, record.Phone) {
		a.after(s.After, func() { a.postReport(record, s) })
	}
}

func (a *app) postReport(record *sendRecord, s step) {
	body, err := cmfake.StatusReportJSON(cmfake.StatusReport{
		Reference:        record.Reference,
		Phone:            record.Phone,
		Status:           s.Status,
		ErrorCode:        s.ErrorCode,
		ErrorDescription: s.Description,
		At:               time.Now(),
	})
	entry := report{Status: string(s.Status), ErrorCode: s.ErrorCode, At: time.Now().UTC()}
	if err == nil {
		entry.BackendStatus, err = a.postWebhook("status", body)
	}
	if err != nil {
		entry.Error = err.Error()
	}
	log.Printf("[cmfake] status %s for %s: backend %d %s", s.Status, record.Reference, entry.BackendStatus, entry.Error)
	a.mu.Lock()
	record.Reports = append(record.Reports, entry)
	a.mu.Unlock()
}

func (a *app) postWebhook(kind string, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, a.webhookBase+"/"+kind, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", sourceIP)
	req.Header.Set("CF-Connecting-IP", sourceIP)
	res, err := a.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode, nil
}

func (a *app) snapshot() []sendRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]sendRecord, len(a.sends))
	for i, s := range a.sends {
		out[i] = *s
		out[i].Buttons = append([]button(nil), s.Buttons...)
		out[i].Reports = append([]report(nil), s.Reports...)
	}
	return out
}

func (a *app) find(reference string) (sendRecord, error) {
	for _, s := range a.snapshot() {
		if s.Reference == reference {
			return s, nil
		}
	}
	return sendRecord{}, errNotFound
}

func (a *app) listSends(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.snapshot())
}

type inboundRequest struct {
	Phone     string `json:"phone"`
	To        string `json:"to"`
	Channel   string `json:"channel"`
	Text      string `json:"text"`
	InReplyTo string `json:"in_reply_to"`
}

type tapRequest struct {
	Reference string `json:"reference"`
	Postback  string `json:"postback"`
	Label     string `json:"label"`
	Phone     string `json:"phone"`
}

func readControl(r *http.Request, out any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxControlBytes))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func (a *app) inbound(w http.ResponseWriter, r *http.Request) {
	var in inboundRequest
	if err := readControl(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	channel := carriermessaging.Channel(strings.ToLower(strings.TrimSpace(in.Channel)))
	if !channel.Valid() {
		writeError(w, http.StatusBadRequest, fmt.Errorf("channel must be rcs or sms, got %q", in.Channel))
		return
	}
	if strings.TrimSpace(in.Text) == "" || strings.TrimSpace(in.To) == "" {
		writeError(w, http.StatusBadRequest, errors.New("text and to are required"))
		return
	}
	body, err := cmfake.InboundJSON(cmfake.InboundMessage{
		Reference: uuid.NewString(),
		Phone:     in.Phone,
		Channel:   channel,
		To:        strings.TrimSpace(in.To),
		Text:      in.Text,
		InReplyTo: strings.TrimSpace(in.InReplyTo),
		At:        time.Now(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.forward(w, "inbound", body)
}

func (a *app) tap(w http.ResponseWriter, r *http.Request) {
	var in tapRequest
	if err := readControl(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Postback) == "" {
		writeError(w, http.StatusBadRequest, errors.New("postback is required"))
		return
	}
	sent, err := a.find(strings.TrimSpace(in.Reference))
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no send with reference %q", in.Reference))
		return
	}
	if sent.Channel != channelRCS {
		writeError(w, http.StatusBadRequest, errors.New("only RCS messages have buttons"))
		return
	}
	phone := strings.TrimSpace(in.Phone)
	if phone == "" {
		phone = sent.Phone
	}
	body, err := cmfake.SuggestionClickJSON(cmfake.SuggestionClick{
		EventID:   uuid.NewString(),
		Reference: sent.Reference,
		Phone:     phone,
		To:        sent.From,
		Postback:  in.Postback,
		Label:     in.Label,
		At:        time.Now(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.forward(w, "events", body)
}

func (a *app) forward(w http.ResponseWriter, kind string, body []byte) {
	status, err := a.postWebhook(kind, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	log.Printf("[cmfake] %s posted: backend %d", kind, status)
	writeJSON(w, http.StatusOK, map[string]any{"backend_status": status})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func only(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("use %s", method))
			return
		}
		handler(w, r)
	}
}
