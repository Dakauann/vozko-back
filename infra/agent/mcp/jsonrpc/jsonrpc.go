package jsonrpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ErrorObject) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	URL         string
	HTTP        HTTPClient
	BearerToken string

	SessionID   string
	ExtraHeader map[string]string
	ID          int
}

func Call(ctx context.Context, opts Options, method string, params any, out any) (http.Header, error) {
	if opts.HTTP == nil {
		return nil, errors.New("jsonrpc: HTTPClient required")
	}
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal params: %w", err)
		}
		rawParams = b
	}
	req := Request{JSONRPC: "2.0", ID: opts.ID, Method: method, Params: rawParams}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: new http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if opts.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+opts.BearerToken)
	}
	if opts.SessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", opts.SessionID)
	}
	for k, v := range opts.ExtraHeader {
		httpReq.Header.Set(k, v)
	}
	res, err := opts.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: transport: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusAccepted {
		_, _ = io.Copy(io.Discard, res.Body)
		return res.Header, nil
	}
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		return res.Header, fmt.Errorf("jsonrpc: http %d: %s", res.StatusCode, truncate(string(data), 256))
	}

	rpcRes, err := readResponse(res, opts.ID)
	if err != nil {
		return res.Header, err
	}
	if rpcRes.Error != nil {
		return res.Header, rpcRes.Error
	}
	if out != nil && len(rpcRes.Result) > 0 {
		if err := json.Unmarshal(rpcRes.Result, out); err != nil {
			return res.Header, fmt.Errorf("jsonrpc: decode result: %w", err)
		}
	}
	return res.Header, nil
}

func readResponse(res *http.Response, wantID int) (*Response, error) {
	ct := res.Header.Get("Content-Type")
	mediaType := ct
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		mediaType = strings.TrimSpace(ct[:i])
	}
	switch strings.ToLower(mediaType) {
	case "text/event-stream":
		return readSSEResponse(res.Body, wantID)
	default:

		var rpcRes Response
		if err := json.NewDecoder(res.Body).Decode(&rpcRes); err != nil {
			return nil, fmt.Errorf("jsonrpc: decode response: %w", err)
		}
		return &rpcRes, nil
	}
}

func readSSEResponse(body io.Reader, wantID int) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var dataBuf strings.Builder
	flush := func() (*Response, bool, error) {
		if dataBuf.Len() == 0 {
			return nil, false, nil
		}
		payload := dataBuf.String()
		dataBuf.Reset()
		var rpcRes Response
		if err := json.Unmarshal([]byte(payload), &rpcRes); err != nil {

			return nil, false, nil
		}
		if rpcRes.ID == wantID && (rpcRes.Error != nil || rpcRes.Result != nil) {
			return &rpcRes, true, nil
		}
		return nil, false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if r, done, err := flush(); err != nil {
				return nil, err
			} else if done {
				return r, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		if field == "data" {
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("jsonrpc: read sse: %w", err)
	}

	if r, done, err := flush(); err != nil {
		return nil, err
	} else if done {
		return r, nil
	}
	return nil, errors.New("jsonrpc: sse stream closed without response")
}

func Notify(ctx context.Context, opts Options, method string, params any) (http.Header, error) {
	if opts.HTTP == nil {
		return nil, errors.New("jsonrpc: HTTPClient required")
	}
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: marshal params: %w", err)
		}
		rawParams = b
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{JSONRPC: "2.0", Method: method, Params: rawParams})
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: marshal notification: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: new http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if opts.BearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+opts.BearerToken)
	}
	if opts.SessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", opts.SessionID)
	}
	for k, v := range opts.ExtraHeader {
		httpReq.Header.Set(k, v)
	}
	res, err := opts.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: transport: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return res.Header, fmt.Errorf("jsonrpc: http %d on notification", res.StatusCode)
	}
	return res.Header, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
