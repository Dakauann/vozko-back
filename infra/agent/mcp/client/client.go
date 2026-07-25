package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/jsonrpc"
)

type Client struct {
	URL         string
	HTTP        jsonrpc.HTTPClient
	BearerToken string
	HeaderName  string
	Prefix      string
	APIKey      string
	sessionID string
}

func New(url string) *Client {
	return &Client{URL: url, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Initialize(ctx context.Context, clientName, clientVersion string) error {
	params := map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": clientVersion},
	}
	h, err := jsonrpc.Call(ctx, c.options(1), "initialize", params, nil)
	if err != nil {
		return err
	}
	if sid := h.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}
	if _, err := jsonrpc.Notify(ctx, c.options(0), "notifications/initialized", map[string]any{}); err != nil {
		_ = err
	}
	return nil
}

func (c *Client) ListTools(ctx context.Context) ([]mcp.Tool, error) {
	var out struct {
		Tools []struct {
			Name        string          `json:"name"`
			Title       string          `json:"title"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if _, err := jsonrpc.Call(ctx, c.options(2), "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	tools := make([]mcp.Tool, 0, len(out.Tools))
	for _, t := range out.Tools {
		tools = append(tools, mcp.Tool{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: append([]byte(nil), t.InputSchema...),
		})
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (mcp.ToolResult, error) {
	params := map[string]any{"name": name, "arguments": args}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if _, err := jsonrpc.Call(ctx, c.options(3), "tools/call", params, &out); err != nil {
		return mcp.ToolResult{}, err
	}
	if len(out.Content) == 0 {
		return mcp.ToolResult{}, errors.New("mcp: empty content in tools/call result")
	}
	res := mcp.ToolResult{IsError: out.IsError}
	for _, c := range out.Content {
		res.Content = append(res.Content, mcp.ToolContent{Type: c.Type, Text: c.Text})
	}
	return res, nil
}

func (c *Client) options(id int) jsonrpc.Options {
	o := jsonrpc.Options{URL: c.URL, HTTP: c.HTTP, ID: id, BearerToken: c.BearerToken, SessionID: c.sessionID}
	if c.APIKey != "" {
		header := c.HeaderName
		if header == "" {
			header = "Authorization"
		}
		prefix := c.Prefix
		if prefix == "" && header == "Authorization" {
			prefix = "Bearer "
		}
		o.ExtraHeader = map[string]string{header: prefix + c.APIKey}
	}
	return o
}
