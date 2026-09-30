package uazapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	uw "vozko/domain/unofficial_whatsapp"
)

var _ uw.HistoryAPI = (*Client)(nil)

type findMessagesRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type findMessagesResponse struct {
	Messages json.RawMessage `json:"messages"`
}

func (c *Client) FindMessages(ctx context.Context, ref uw.InstanceRef, in uw.FindMessagesInput) (*uw.MessagePage, error) {
	if in.Limit < 1 {
		return nil, fmt.Errorf("uazapi: message page limit must be at least 1, got %d", in.Limit)
	}

	var resp findMessagesResponse
	err := c.instanceCall(ctx, ref, http.MethodPost, "/message/find",
		findMessagesRequest{Limit: in.Limit, Offset: in.Offset}, &resp)
	if err != nil {
		return nil, err
	}
	return &uw.MessagePage{Messages: resp.Messages, Returned: uw.ItemCount(resp.Messages)}, nil
}
