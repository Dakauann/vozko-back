package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
)

var editedComponentTypes = map[string]bool{"HEADER": true, "BODY": true, "FOOTER": true, "BUTTONS": true}

var editableButtonTypes = map[string]bool{"QUICK_REPLY": true, "URL": true, "PHONE_NUMBER": true}

type storedComponent struct {
	Type    string            `json:"type"`
	Buttons []json.RawMessage `json:"buttons,omitempty"`
}

type editTemplateRequest struct {
	Category   string            `json:"category,omitempty"`
	Components []json.RawMessage `json:"components"`
}

func (c *Client) EditTemplate(ctx context.Context, templateID string, input conversation.EditTemplateInput) error {
	if c == nil || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}
	if c.templatesChannelScoped {
		return template.ErrEditUnsupported
	}
	if strings.TrimSpace(templateID) == "" {
		return conversation.ErrWhatsAppTemplateIDRequired
	}
	stored, err := c.storedComponents(ctx, templateID)
	if err != nil {
		return err
	}
	components, err := mergeEditedComponents(stored, toWireComponents(input.Components))
	if err != nil {
		return err
	}
	body, err := json.Marshal(editTemplateRequest{Category: strings.ToUpper(strings.TrimSpace(input.Category)), Components: components})
	if err != nil {
		return err
	}
	_, err = c.templateRequest(ctx, http.MethodPost, fmt.Sprintf("%s/%s", c.baseURL, templateID), body, "edit template")
	return err
}

func (c *Client) storedComponents(ctx context.Context, templateID string) ([]json.RawMessage, error) {
	respBody, err := c.templateRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%s?fields=components", c.baseURL, templateID), nil, "read template for edit")
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Components []json.RawMessage `json:"components"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}
	return decoded.Components, nil
}

func (c *Client) templateRequest(ctx context.Context, method, endpoint string, body []byte, op string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newTemplateAPIError(op, resp.StatusCode, respBody)
	}
	return respBody, nil
}

func mergeEditedComponents(stored []json.RawMessage, edited []createTemplateComponent) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(edited)+len(stored))
	for _, comp := range edited {
		encoded, err := json.Marshal(comp)
		if err != nil {
			return nil, err
		}
		out = append(out, encoded)
	}
	for _, raw := range stored {
		var comp storedComponent
		if err := json.Unmarshal(raw, &comp); err != nil {
			return nil, err
		}
		kind := strings.ToUpper(strings.TrimSpace(comp.Type))
		if kind == "BUTTONS" && !buttonsAreEditable(comp.Buttons) {
			return nil, template.ErrEditUnsupported
		}
		if !editedComponentTypes[kind] {
			out = append(out, raw)
		}
	}
	return out, nil
}

func buttonsAreEditable(buttons []json.RawMessage) bool {
	for _, raw := range buttons {
		var button struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &button); err != nil || !editableButtonTypes[strings.ToUpper(strings.TrimSpace(button.Type))] {
			return false
		}
	}
	return true
}
