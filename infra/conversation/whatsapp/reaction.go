package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/lead"
)

var errReactionTargetRequired = errors.New("whatsapp reaction: the target message id is required")

type sendReactionRequest struct {
	MessagingProduct string          `json:"messaging_product"`
	RecipientType    string          `json:"recipient_type"`
	To               string          `json:"to"`
	Type             string          `json:"type"`
	Reaction         reactionContent `json:"reaction"`
}

type reactionContent struct {
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

func (c *Client) SendReaction(ctx context.Context, to, messageID, emoji string) error {
	if strings.TrimSpace(to) == "" {
		return conversation.ErrWhatsAppRecipientRequired
	}
	if strings.TrimSpace(messageID) == "" {
		return errReactionTargetRequired
	}
	if c == nil || c.phoneNumberID == "" || c.accessToken == "" {
		return conversation.ErrWhatsAppClientDisabled
	}

	body, err := json.Marshal(sendReactionRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               lead.NormalizeWhatsAppNumber(to),
		Type:             "reaction",
		Reaction:         reactionContent{MessageID: strings.TrimSpace(messageID), Emoji: strings.TrimSpace(emoji)},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messagesEndpoint(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp send reaction failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	return nil
}

var _ conversation.WhatsAppReactionClient = (*Client)(nil)
