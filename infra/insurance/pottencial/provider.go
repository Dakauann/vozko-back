package pottencial

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"vozko/domain/insurance"
)

const (
	DefaultBaseURL = "https://api-sandbox.pottencial.com.br"

	quotesPathFormat = "/insurance/v1/%s/quotes"
	oauthPath        = "/oauth/v3/access-token"

	judicialExecutionFiscalProductKey = "judicial-execucao-fiscal"
	imobiliarioProductKey             = "imobiliario"
	defaultPolicyType                 = "Unique"
	defaultPropertyRiskType           = "Property"
	accessTokenSafetyWindow           = 30 * time.Second
)

type payloadBuilder func(insurance.ProviderQuoteRequest) (interface{}, error)
type responseMapper func(insurance.ProviderQuoteRequest, quoteResponse) (*insurance.InsuranceQuote, error)

type policyHandler struct {
	productKey   string
	buildPayload payloadBuilder
	mapResponse  responseMapper
}

var policyHandlers = map[insurance.PolicyType]policyHandler{
	insurance.PolicyTypeJudicialExecutionFiscal: {
		productKey:   judicialExecutionFiscalProductKey,
		buildPayload: buildJudicialQuotePayload,
		mapResponse:  mapQuoteResponse,
	},
	insurance.PolicyTypeImobiliario: {
		productKey:   imobiliarioProductKey,
		buildPayload: buildRealEstateQuotePayload,
		mapResponse:  mapQuoteResponse,
	},
}

type Config struct {
	BaseURL      string
	OAuthURL     string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

type Provider struct {
	baseURL      string
	oauthURL     string
	clientID     string
	clientSecret string
	httpClient   *http.Client

	commissionedAgents []commissionedAgent

	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewProvider(cfg Config) (*Provider, error) {
	clientID := strings.TrimSpace(cfg.ClientID)
	clientSecret := strings.TrimSpace(cfg.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("pottencial: client credentials are required")
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	oauthURL := strings.TrimSpace(cfg.OAuthURL)
	if oauthURL == "" {
		oauthURL = baseURL + oauthPath
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}

	return &Provider{
		baseURL:            baseURL,
		oauthURL:           oauthURL,
		clientID:           clientID,
		clientSecret:       clientSecret,
		httpClient:         httpClient,
		commissionedAgents: cloneCommissionedAgents(defaultCommissionedAgents),
	}, nil
}

func (p *Provider) Provider() insurance.InsuranceProvider {
	return insurance.InsuranceProviderPottencial
}

func (p *Provider) Supports(policy insurance.PolicyType) bool {
	_, ok := policyHandlers[policy]
	return ok
}

func (p *Provider) RequiredFields(policy insurance.PolicyType) ([]insurance.RequiredField, error) {
	if !p.Supports(policy) {
		return nil, insurance.ErrUnsupportedPolicyType
	}
	return insurance.RequiredFieldsForPolicy(policy)
}

func (p *Provider) Quote(ctx context.Context, req insurance.ProviderQuoteRequest) (*insurance.InsuranceQuote, error) {
	handler, ok := policyHandlers[req.PolicyType]
	if !ok {
		return nil, insurance.ErrUnsupportedPolicyType
	}

	req = p.ensureCommissionedAgents(req)

	payload, err := handler.buildPayload(req)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("pottencial: failed to marshal quote request: %w", err)
	}

	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := p.baseURL + fmt.Sprintf(quotesPathFormat, handler.productKey)
	apiReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("pottencial: failed to build quote request: %w", err)
	}
	apiReq.Header.Set("Content-Type", "application/json")
	apiReq.Header.Set("client_id", p.clientID)
	apiReq.Header.Set("access_token", token)

	resp, err := p.httpClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("pottencial: quote request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, p.parseAPIError("quote", resp)
	}

	decoder := json.NewDecoder(resp.Body)
	var payloadResp quoteResponse
	if err := decoder.Decode(&payloadResp); err != nil {
		return nil, fmt.Errorf("pottencial: unable to decode quote response: %w", err)
	}

	return handler.mapResponse(req, payloadResp)
}

func (p *Provider) ensureCommissionedAgents(req insurance.ProviderQuoteRequest) insurance.ProviderQuoteRequest {
	if len(p.commissionedAgents) == 0 {
		return req
	}

	if req.Details == nil {
		req.Details = make(map[string]interface{})
	} else if existing, ok := req.Details["commissionedAgents"]; ok {
		switch v := existing.(type) {
		case []commissionedAgent:
			if len(v) > 0 {
				return req
			}
		case []map[string]interface{}:
			if len(v) > 0 {
				return req
			}
		case []interface{}:
			if len(v) > 0 {
				return req
			}
		}
	}

	req.Details["commissionedAgents"] = cloneCommissionedAgents(p.commissionedAgents)
	return req
}

func (p *Provider) getAccessToken(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry) {
		token := p.accessToken
		p.tokenMu.Unlock()
		return token, nil
	}
	p.tokenMu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.oauthURL, nil)
	if err != nil {
		return "", fmt.Errorf("pottencial: failed to build oauth request: %w", err)
	}
	request.SetBasicAuth(p.clientID, p.clientSecret)
	request.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("pottencial: oauth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", p.parseAPIError("oauth", resp)
	}

	var payload oauthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("pottencial: unable to decode oauth response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", fmt.Errorf("pottencial: oauth response missing access token")
	}

	expiresIn := time.Duration(payload.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	expiry := time.Now().Add(expiresIn - accessTokenSafetyWindow)
	if time.Until(expiry) <= 0 {
		expiry = time.Now().Add(expiresIn / 2)
	}

	p.tokenMu.Lock()
	p.accessToken = payload.AccessToken
	p.tokenExpiry = expiry
	p.tokenMu.Unlock()

	return payload.AccessToken, nil
}

func (p *Provider) parseAPIError(operation string, resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var envelope errorEnvelope
	if len(data) > 0 {
		_ = json.Unmarshal(data, &envelope)
	}

	message := strings.TrimSpace(string(data))
	if envelope.Message != "" {
		message = envelope.Message
	} else if len(envelope.Errors) > 0 {
		message = envelope.Errors[0].Message
	}

	if message == "" {
		message = resp.Status
	}

	return fmt.Errorf("pottencial: %s request failed with status %d: %s", operation, resp.StatusCode, message)
}

type oauthResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type errorEnvelope struct {
	Message string         `json:"message"`
	Errors  []errorMessage `json:"errors"`
}

type errorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
