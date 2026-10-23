package cmfake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/infra/cmcom"
)

const (
	RCSChannelTypeID  = "5d0c7a8e-1b2f-4c3d-9e4f-a5b6c7d8e9f0"
	ProfileCategoryID = "6e1d8b9f-2c30-4d4e-8f50-b6c7d8e9f0a1"
	UnreachableDetail = "Message not delivered because recipient is not capable to receive a message from this channel"
	sendPath          = "/v1.0/message"
	channelsPrefix    = "/channels/v1/accounts/"
	tokenHeader       = "X-CM-PRODUCTTOKEN"
	statePending      = "PENDING"
	formComplete      = "COMPLETE"
	formIncomplete    = "INCOMPLETE"
)

type Request struct {
	Method       string
	Path         string
	Query        url.Values
	ProductToken string
	Header       http.Header
	Body         []byte
}

type rawResponse struct {
	status int
	body   string
}

type profile struct {
	requestID string
	statuses  []ProfileStatus
	inputs    map[string]string
}

type Fake struct {
	URL          string
	ProductToken string
	AccountID    string

	server *httptest.Server

	mu           sync.Mutex
	requests     []Request
	scripts      map[string]Outcome
	rawSend      *rawResponse
	rawChannels  *rawResponse
	channelTypes []ChannelType
	categories   []Category
	form         []FormElement
	forcedState  string
	failure      sendFailure
	channelReqs  map[string]bool
	profiles     map[string]*profile
	outcome      func(phone string) Outcome
	onSend       func(SendResult)
	progression  []ProfileStatus
}

type Options struct {
	ProductToken       string
	AccountID          string
	Outcome            func(phone string) Outcome
	OnSend             func(SendResult)
	ProfileProgression []ProfileStatus
}

func Open(o Options) (*Fake, error) {
	if err := (cmcom.Config{ProductToken: o.ProductToken, AccountID: o.AccountID}).Validate(); err != nil {
		return nil, err
	}
	f := newFake(o.ProductToken, o.AccountID)
	f.outcome, f.onSend = o.Outcome, o.OnSend
	f.progression = append([]ProfileStatus(nil), o.ProfileProgression...)
	return f, nil
}

func New() *Fake {
	f := newFake("3a9e2c1d-4b5f-4a6e-9d7c-8b1a2f3e4d5c", "7b6a5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d")
	f.server = httptest.NewServer(f)
	f.URL = f.server.URL
	return f
}

func newFake(token, account string) *Fake {
	return &Fake{
		ProductToken: token,
		AccountID:    account,
		scripts:      map[string]Outcome{},
		channelTypes: []ChannelType{{ID: "1f2e3d4c-5b6a-4978-8695-a4b3c2d1e0f9", PrettyName: "WhatsApp"}, {ID: RCSChannelTypeID, PrettyName: "RCS"}},
		categories:   []Category{{ID: "2a3b4c5d-6e7f-4809-9a1b-2c3d4e5f6a7b", Name: "Account"}, {ID: ProfileCategoryID, Name: "Profile"}},
		form:         DefaultForm(),
		channelReqs:  map[string]bool{},
		profiles:     map[string]*profile{},
	}
}

func (f *Fake) Close() {
	if f.server != nil {
		f.server.Close()
	}
}

func (f *Fake) Config() cmcom.Config {
	return cmcom.Config{
		ProductToken: f.ProductToken,
		AccountID:    f.AccountID,
		GatewayURL:   f.URL,
		APIURL:       f.URL,
	}
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.requests))
	for i, r := range f.requests {
		out[i] = r
		out[i].Query = cloneValues(r.Query)
		out[i].Header = r.Header.Clone()
		out[i].Body = bytes.Clone(r.Body)
	}
	return out
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func (f *Fake) RespondRaw(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rawSend = rawOverride(status, body)
}

func (f *Fake) RespondChannelsRaw(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rawChannels = rawOverride(status, body)
}

func rawOverride(status int, body string) *rawResponse {
	if status == 0 {
		return nil
	}
	return &rawResponse{status: status, body: body}
}

type sendFailure int

const (
	sendsAnswer sendFailure = iota
	sendsHang
	sendsDrop
)

const hangLimit = 10 * time.Second

func (f *Fake) HangSends() {
	f.setSendFailure(sendsHang)
}

func (f *Fake) DropSends() {
	f.setSendFailure(sendsDrop)
}

func (f *Fake) AnswerSends() {
	f.setSendFailure(sendsAnswer)
}

func (f *Fake) setSendFailure(mode sendFailure) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failure = mode
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	isSend := r.URL.Path == sendPath && r.Method == http.MethodPost
	f.mu.Lock()
	f.requests = append(f.requests, Request{
		Method:       r.Method,
		Path:         r.URL.Path,
		Query:        r.URL.Query(),
		ProductToken: r.Header.Get(tokenHeader),
		Header:       r.Header.Clone(),
		Body:         body,
	})
	failure := f.failure
	f.mu.Unlock()
	if isSend && failure == sendsHang {
		select {
		case <-r.Context().Done():
		case <-time.After(hangLimit):
		}
		return
	}
	if isSend && failure == sendsDrop {
		dropConnection(w)
		return
	}
	if isSend {
		f.mu.Lock()
		answer := f.answerSend(r, body)
		onSend := f.onSend
		f.mu.Unlock()
		if onSend != nil {
			for _, result := range answer.results {
				onSend(result)
			}
		}
		answer.write(w)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.HasPrefix(r.URL.Path, channelsPrefix):
		f.serveChannels(w, r, body)
	default:
		writeJSON(w, http.StatusNotFound, problem("Not Found", http.StatusNotFound))
	}
}

func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		panic("cmfake: the server cannot drop connections")
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		panic(fmt.Sprintf("cmfake: drop connection: %v", err))
	}
	_ = conn.Close()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRaw(w http.ResponseWriter, raw *rawResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(raw.status)
	_, _ = io.WriteString(w, raw.body)
}

func problem(title string, status int) map[string]any {
	return map[string]any{"type": "about:blank", "title": title, "status": status}
}

func newID() string {
	return uuid.NewString()
}
