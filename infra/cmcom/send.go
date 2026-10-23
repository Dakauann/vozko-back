package cmcom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/domain/carriermessaging"
)

const (
	sendPath            = "/v1.0/message"
	channelRCS          = "RCS"
	channelSMS          = "SMS"
	bodyTypeAuto        = "Auto"
	statusAccepted      = "Accepted"
	statusRejected      = "Rejected"
	resultOK            = 0
	resultMessageErrors = 201
	maxValidity         = 24 * time.Hour

	UnreachableErrorCode = "42"
)

var ErrRequestInvalid = errors.New("cmcom: invalid send request")

type wireRequest struct {
	Messages wireEnvelope `json:"messages"`
}

type wireEnvelope struct {
	Msg []wireMessage `json:"msg"`
}

type wireMessage struct {
	From            string           `json:"from"`
	To              []wireRecipient  `json:"to"`
	Body            wireBody         `json:"body"`
	Reference       string           `json:"reference"`
	AllowedChannels []string         `json:"allowedChannels"`
	Validity        string           `json:"validity,omitempty"`
	RichContent     *wireRichContent `json:"richContent,omitempty"`
}

type wireRecipient struct {
	Number string `json:"number"`
}

type wireBody struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type wireSendResult struct {
	Details   string              `json:"details"`
	ErrorCode *int                `json:"errorCode"`
	Messages  []wireMessageResult `json:"messages"`
}

type wireMessageResult struct {
	To               string  `json:"to"`
	Status           string  `json:"status"`
	Reference        *string `json:"reference"`
	MessageDetails   *string `json:"messageDetails"`
	MessageErrorCode *int    `json:"messageErrorCode"`
}

func (c *Client) Send(ctx context.Context, req carriermessaging.SendRequest) (carriermessaging.SendOutcome, error) {
	if err := req.Content.Validate(); err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	reference, err := checkedReference(req.Reference)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	from := strings.TrimSpace(req.SenderProfileID)
	if from == "" {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: no sender profile", ErrRequestInvalid)
	}
	to, err := GatewayNumber(req.Phone)
	if err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	validity, err := validityFor(req.Validity)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	rich, err := richContentFor(req.Content)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	text, err := fallbackText(req.Content)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	return c.post(ctx, wireMessage{
		From:            from,
		To:              []wireRecipient{{Number: to}},
		Body:            wireBody{Type: bodyTypeAuto, Content: text},
		Reference:       reference,
		AllowedChannels: []string{channelRCS},
		Validity:        validity,
		RichContent:     &rich,
	})
}

func (c *Client) SendSMS(ctx context.Context, req carriermessaging.SMSRequest) (carriermessaging.SendOutcome, error) {
	from, err := routeNumber(req.From)
	if err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(req.Text) == "" {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: the SMS has no text", ErrRequestInvalid)
	}
	reference, err := checkedReference(req.Reference)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	to, err := GatewayNumber(req.Phone)
	if err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", ErrRequestInvalid, err)
	}
	return c.post(ctx, wireMessage{
		From:            from,
		To:              []wireRecipient{{Number: to}},
		Body:            wireBody{Type: bodyTypeAuto, Content: req.Text},
		Reference:       reference,
		AllowedChannels: []string{channelSMS},
	})
}

func checkedReference(raw string) (string, error) {
	reference, err := carriermessaging.ReferenceFor(raw)
	if err != nil || reference != raw {
		return "", fmt.Errorf("%w: %w", ErrRequestInvalid, carriermessaging.ErrReferenceInvalid)
	}
	return reference, nil
}

func validityFor(d time.Duration) (string, error) {
	if d <= 0 || d > maxValidity || d%time.Minute != 0 {
		return "", fmt.Errorf("%w: the validity must be whole minutes up to 24 hours, got %s", ErrRequestInvalid, d)
	}
	return strconv.FormatInt(int64(d/time.Minute), 10) + "m", nil
}

func (c *Client) post(ctx context.Context, msg wireMessage) (carriermessaging.SendOutcome, error) {
	res, err := c.do(ctx, http.MethodPost, c.gatewayURL+sendPath, wireRequest{Messages: wireEnvelope{Msg: []wireMessage{msg}}}, carriermessaging.ErrSendOutcomeUnknown)
	if err != nil {
		return carriermessaging.SendOutcome{}, err
	}
	return sendOutcome(res, msg.Reference)
}

func sendOutcome(res response, reference string) (carriermessaging.SendOutcome, error) {
	switch res.status {
	case http.StatusOK:
		outcome, err := acceptedAnswer(res.body, reference)
		if err != nil {
			return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", carriermessaging.ErrSendOutcomeUnknown, err)
		}
		return outcome, nil
	case http.StatusBadRequest:
		outcome, err := refusedAnswer(res.body, reference)
		if err != nil {
			return carriermessaging.SendOutcome{}, fmt.Errorf("%w: %w", carriermessaging.ErrProviderRefused, err)
		}
		return outcome, nil
	}
	return carriermessaging.SendOutcome{}, statusError(res, "send", carriermessaging.ErrSendOutcomeUnknown)
}

func acceptedAnswer(body []byte, reference string) (carriermessaging.SendOutcome, error) {
	var result wireSendResult
	if err := json.Unmarshal(body, &result); err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: send answered 200 with %q", ErrProviderResponse, body)
	}
	if result.ErrorCode == nil || (*result.ErrorCode != resultOK && *result.ErrorCode != resultMessageErrors) {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: send answered 200 with error code %v", ErrProviderResponse, result.ErrorCode)
	}
	return messageOutcome(result.Messages, reference)
}

func refusedAnswer(body []byte, reference string) (carriermessaging.SendOutcome, error) {
	var result wireSendResult
	if err := json.Unmarshal(body, &result); err != nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("send answered 400 with %q", body)
	}
	if result.ErrorCode == nil || *result.ErrorCode != resultMessageErrors || len(result.Messages) == 0 {
		return carriermessaging.SendOutcome{}, fmt.Errorf("send answered 400 with error code %v: %s", result.ErrorCode, result.Details)
	}
	outcome, err := messageOutcome(result.Messages, reference)
	if err == nil && outcome.Accepted {
		return carriermessaging.SendOutcome{}, fmt.Errorf("send answered 400 for an accepted message")
	}
	return outcome, err
}

func messageOutcome(results []wireMessageResult, reference string) (carriermessaging.SendOutcome, error) {
	if len(results) != 1 {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: expected one message result, got %d", ErrProviderResponse, len(results))
	}
	r := results[0]
	if r.Reference != nil && *r.Reference != "" && *r.Reference != reference {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: the result is for reference %q", ErrProviderResponse, *r.Reference)
	}
	if r.MessageErrorCode == nil {
		return carriermessaging.SendOutcome{}, fmt.Errorf("%w: the message result has no error code", ErrProviderResponse)
	}
	code := *r.MessageErrorCode
	switch {
	case r.Status == statusAccepted && code == resultOK:
		return carriermessaging.SendOutcome{Accepted: true}, nil
	case r.Status == statusRejected && code != resultOK:
		detail := ""
		if r.MessageDetails != nil {
			detail = *r.MessageDetails
		}
		errorCode := strconv.Itoa(code)
		return carriermessaging.SendOutcome{ErrorCode: errorCode, Detail: detail, Unreachable: errorCode == UnreachableErrorCode}, nil
	}
	return carriermessaging.SendOutcome{}, fmt.Errorf("%w: message status %q with error code %d", ErrProviderResponse, r.Status, code)
}
