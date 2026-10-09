package viacep

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vozko/domain/address"
	"vozko/domain/cep"
)

const (
	DefaultBaseURL   = "https://viacep.com.br/ws"
	defaultTimeout   = 5 * time.Second
	maxResponseBytes = 64 << 10
)

var _ cep.Lookup = (*Client)(nil)

type Config struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type Client struct {
	baseURL string
	timeout time.Duration
	http    *http.Client
}

type StatusError struct {
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("viacep: status %d", e.StatusCode)
}

func (e *StatusError) Unwrap() error {
	return cep.ErrUnavailable
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{baseURL: baseURL, timeout: timeout, http: httpClient}
}

func (c *Client) Lookup(ctx context.Context, raw string) (*cep.CEPInfo, error) {
	code, err := cep.Parse(raw)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+code+"/json/", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", cep.ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", cep.ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusBadRequest:
		return nil, cep.ErrInvalidCEP
	case resp.StatusCode != http.StatusOK:
		return nil, &StatusError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", cep.ErrUnavailable, err)
	}
	var payload response
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: unreadable body: %v", cep.ErrUnavailable, err)
	}
	if payload.Erro {
		return nil, cep.ErrNotFound
	}
	if strings.TrimSpace(payload.Localidade) == "" || strings.TrimSpace(payload.UF) == "" {
		return nil, fmt.Errorf("%w: body without city or state", cep.ErrUnavailable)
	}
	return payload.info(code), nil
}

type response struct {
	Logradouro  string `json:"logradouro"`
	Complemento string `json:"complemento"`
	Bairro      string `json:"bairro"`
	Localidade  string `json:"localidade"`
	UF          string `json:"uf"`
	IBGE        string `json:"ibge"`
	Erro        flag   `json:"erro"`
}

func (r response) info(code string) *cep.CEPInfo {
	cityCode := strings.TrimSpace(r.IBGE)
	if !address.ValidCityCode(cityCode) {
		cityCode = ""
	}
	return &cep.CEPInfo{
		Cep:        code,
		Logradouro: strings.TrimSpace(r.Logradouro),
		Complement: strings.TrimSpace(r.Complemento),
		Bairro:     strings.TrimSpace(r.Bairro),
		Localidade: strings.TrimSpace(r.Localidade),
		Uf:         strings.TrimSpace(r.UF),
		IBGE:       cityCode,
	}
}

type flag bool

func (f *flag) UnmarshalJSON(data []byte) error {
	*f = flag(bytes.Equal(data, []byte("true")) || bytes.Equal(data, []byte(`"true"`)))
	return nil
}
