package cmfake

import (
	"encoding/json"
	"fmt"
	"net/http"

	"vozko/infra/cmcom"
)

type outcomeKind int

const (
	outcomeAccept outcomeKind = iota
	outcomeReject
)

type Outcome struct {
	kind    outcomeKind
	code    int
	details string
}

func Accept() Outcome {
	return Outcome{kind: outcomeAccept}
}

func Reject(code int, details string) Outcome {
	return Outcome{kind: outcomeReject, code: code, details: details}
}

func Unreachable() Outcome {
	return Reject(42, UnreachableDetail)
}

func (f *Fake) Script(phone string, o Outcome) {
	key, err := cmcom.PhoneFromGateway(phone)
	if err != nil {
		panic(fmt.Sprintf("cmfake: cannot script %q: %v", phone, err))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[key] = o
}

type SentMessage struct {
	ProductToken    string
	From            string
	To              string
	Phone           string
	Reference       string
	Validity        string
	BodyType        string
	BodyContent     string
	AllowedChannels []string
	Raw             json.RawMessage
}

type wireSendRequest struct {
	Messages struct {
		Msg []json.RawMessage `json:"msg"`
	} `json:"messages"`
}

type wireMessage struct {
	From string `json:"from"`
	To   []struct {
		Number string `json:"number"`
	} `json:"to"`
	Body struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"body"`
	Reference       string   `json:"reference"`
	AllowedChannels []string `json:"allowedChannels"`
	Validity        string   `json:"validity"`
}

func parseSend(body []byte) ([]json.RawMessage, []wireMessage, error) {
	var req wireSendRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, err
	}
	msgs := make([]wireMessage, len(req.Messages.Msg))
	for i, raw := range req.Messages.Msg {
		if err := json.Unmarshal(raw, &msgs[i]); err != nil {
			return nil, nil, err
		}
	}
	return req.Messages.Msg, msgs, nil
}

func sentMessages(token string, body []byte) ([]SentMessage, error) {
	raws, msgs, err := parseSend(body)
	if err != nil {
		return nil, err
	}
	var out []SentMessage
	for i, m := range msgs {
		for _, to := range m.To {
			phone, _ := cmcom.PhoneFromGateway(to.Number)
			out = append(out, SentMessage{
				ProductToken:    token,
				From:            m.From,
				To:              to.Number,
				Phone:           phone,
				Reference:       m.Reference,
				Validity:        m.Validity,
				BodyType:        m.Body.Type,
				BodyContent:     m.Body.Content,
				AllowedChannels: m.AllowedChannels,
				Raw:             raws[i],
			})
		}
	}
	return out, nil
}

func (f *Fake) Sends() []SentMessage {
	var out []SentMessage
	for _, r := range f.Requests() {
		if r.Path != sendPath || r.Method != http.MethodPost {
			continue
		}
		sent, err := sentMessages(r.ProductToken, r.Body)
		if err != nil {
			continue
		}
		out = append(out, sent...)
	}
	return out
}

type SendResult struct {
	Message   SentMessage
	Accepted  bool
	ErrorCode int
	Detail    string
}

type messageResult struct {
	To               string  `json:"to"`
	Status           string  `json:"status"`
	Reference        string  `json:"reference"`
	Parts            int     `json:"parts"`
	MessageDetails   *string `json:"messageDetails"`
	MessageErrorCode int     `json:"messageErrorCode"`
}

type sendResult struct {
	Details   string          `json:"details"`
	ErrorCode int             `json:"errorCode"`
	Messages  []messageResult `json:"messages"`
}

type sendAnswer struct {
	status  int
	body    any
	raw     *rawResponse
	results []SendResult
}

func (a sendAnswer) write(w http.ResponseWriter) {
	if a.raw != nil {
		writeRaw(w, a.raw)
		return
	}
	writeJSON(w, a.status, a.body)
}

func refusedSend(details string, code int) sendAnswer {
	return sendAnswer{status: http.StatusBadRequest, body: sendResult{Details: details, ErrorCode: code, Messages: []messageResult{}}}
}

func (f *Fake) answerSend(r *http.Request, body []byte) sendAnswer {
	token := r.Header.Get(tokenHeader)
	if token != f.ProductToken {
		return refusedSend("No account found for the given authentication/credentials", 101)
	}
	if f.rawSend != nil {
		return sendAnswer{raw: f.rawSend}
	}
	sent, err := sentMessages(token, body)
	if err != nil {
		return refusedSend("Invalid JSON", 202)
	}
	if len(sent) == 0 {
		return refusedSend("A MESSAGES object requires at least one MSG object", 203)
	}
	results := make([]messageResult, len(sent))
	reports := make([]SendResult, len(sent))
	accepted := 0
	for i, m := range sent {
		results[i] = f.resultFor(m.To, m.Reference)
		reports[i] = SendResult{Message: m, Accepted: results[i].Status == "Accepted", ErrorCode: results[i].MessageErrorCode}
		if results[i].MessageDetails != nil {
			reports[i].Detail = *results[i].MessageDetails
		}
		if reports[i].Accepted {
			accepted++
		}
	}
	res := sendResult{Details: fmt.Sprintf("Created %d message(s)", accepted), Messages: results}
	status := http.StatusOK
	switch {
	case accepted == 0:
		res.ErrorCode, status = 201, http.StatusBadRequest
	case accepted < len(results):
		res.ErrorCode = 201
	}
	return sendAnswer{status: status, body: res, results: reports}
}

func (f *Fake) resultFor(number, reference string) messageResult {
	phone, err := cmcom.PhoneFromGateway(number)
	if err != nil {
		detail := fmt.Sprintf("Gsm '%s' is not a number.", number)
		return messageResult{To: number, Status: "Rejected", Reference: reference, MessageDetails: &detail, MessageErrorCode: 303}
	}
	o, scripted := f.scripts[phone]
	if !scripted && f.outcome != nil {
		o = f.outcome(phone)
	}
	if o.kind == outcomeReject {
		detail := o.details
		return messageResult{To: number, Status: "Rejected", Reference: reference, MessageDetails: &detail, MessageErrorCode: o.code}
	}
	return messageResult{To: number, Status: "Accepted", Reference: reference, Parts: 1}
}
