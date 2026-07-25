package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
)

const defaultGraphBaseURL = "https://graph.facebook.com/v22.0"

const metaCallNoPermissionCode = 138006

type CallSignalingClient struct {
	phoneRepo  businessphone.Repository
	httpClient *http.Client
	baseURL    string
}

func NewCallSignalingClient(phoneRepo businessphone.Repository, httpClient *http.Client, baseURL string) *CallSignalingClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultGraphBaseURL
	}
	return &CallSignalingClient{
		phoneRepo:  phoneRepo,
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

func (c *CallSignalingClient) resolvePhone(businessPhoneID string) (metaPhoneNumberID, token string, err error) {
	phone, err := c.phoneRepo.FindByID(businessPhoneID)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(phone.AccessToken) == "" {
		return "", "", conversation.ErrWhatsAppCallNotConfigured
	}
	if strings.TrimSpace(phone.MetaPhoneNumberID) == "" {
		return "", "", conversation.ErrWhatsAppCallNotConfigured
	}
	return phone.MetaPhoneNumberID, phone.AccessToken, nil
}

func (c *CallSignalingClient) InitiateCall(ctx context.Context, in conversation.WhatsAppInitiateCallInput) (string, error) {
	metaPhoneID, token, err := c.resolvePhone(in.PhoneID)
	if err != nil {
		return "", err
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                lead.NormalizeWhatsAppNumber(strings.TrimSpace(in.ToPhoneNumber)),
		"action":            "connect",
		"session": map[string]string{
			"sdp_type": "offer",
			"sdp":      in.SDPOffer,
		},
	}
	if data := strings.TrimSpace(in.CallbackData); data != "" {
		payload["biz_opaque_callback_data"] = data
	}

	respBody, status, err := c.postCalls(ctx, metaPhoneID, token, payload)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		if code := parseMetaErrorCode(respBody); code == metaCallNoPermissionCode {
			return "", conversation.ErrWhatsAppCallNoPermission
		}
		return "", fmt.Errorf("whatsapp initiate call failed: status=%d body=%s", status, string(respBody))
	}

	var decoded struct {
		Calls []struct {
			ID string `json:"id"`
		} `json:"calls"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", fmt.Errorf("parse initiate call response: %w", err)
	}
	if len(decoded.Calls) == 0 || strings.TrimSpace(decoded.Calls[0].ID) == "" {
		return "", fmt.Errorf("initiate call response missing call id: %s", string(respBody))
	}
	return decoded.Calls[0].ID, nil
}

func (c *CallSignalingClient) PreAcceptCall(ctx context.Context, phoneID, callID, sdpAnswer string) error {
	return c.postCallAction(ctx, phoneID, map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "pre_accept",
		"session": map[string]string{
			"sdp_type": "answer",
			"sdp":      sdpAnswer,
		},
	}, "pre_accept")
}

func (c *CallSignalingClient) AcceptCall(ctx context.Context, phoneID, callID, sdpAnswer, callbackData string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "accept",
		"session": map[string]string{
			"sdp_type": "answer",
			"sdp":      sdpAnswer,
		},
	}
	if data := strings.TrimSpace(callbackData); data != "" {
		payload["biz_opaque_callback_data"] = data
	}
	return c.postCallAction(ctx, phoneID, payload, "accept")
}

func (c *CallSignalingClient) RejectCall(ctx context.Context, phoneID, callID string) error {
	return c.postCallAction(ctx, phoneID, map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "reject",
	}, "reject")
}

func (c *CallSignalingClient) postCallAction(ctx context.Context, phoneID string, payload map[string]any, action string) error {
	metaPhoneID, token, err := c.resolvePhone(phoneID)
	if err != nil {
		return err
	}
	respBody, status, err := c.postCalls(ctx, metaPhoneID, token, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("whatsapp %s call failed: status=%d body=%s", action, status, string(respBody))
	}
	return nil
}

func (c *CallSignalingClient) TerminateCall(ctx context.Context, phoneID, callID string) error {
	metaPhoneID, token, err := c.resolvePhone(phoneID)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"call_id":           callID,
		"action":            "terminate",
	}
	respBody, status, err := c.postCalls(ctx, metaPhoneID, token, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("whatsapp terminate call failed: status=%d body=%s", status, string(respBody))
	}
	return nil
}

func (c *CallSignalingClient) postCalls(ctx context.Context, metaPhoneID, token string, payload map[string]any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal calls payload: %w", err)
	}
	endpoint := fmt.Sprintf("%s/%s/calls", c.baseURL, metaPhoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create calls request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("calls request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read calls response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

func parseMetaErrorCode(body []byte) int {
	var decoded struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return 0
	}
	return decoded.Error.Code
}

var _ conversation.WhatsAppCallSignaling = (*CallSignalingClient)(nil)
