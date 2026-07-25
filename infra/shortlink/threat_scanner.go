package shortlink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"vozko/domain/shortlink"
)

const defaultSafeBrowsingEndpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"

type noopThreatScanner struct{}

func (noopThreatScanner) Scan(ctx context.Context, rawURL string) (shortlink.ThreatVerdict, error) {
	return shortlink.ThreatVerdict{Safe: true}, nil
}

type safeBrowsingScanner struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

func NewThreatScanner(apiKey, endpoint string, client *http.Client) shortlink.ThreatScanner {
	if apiKey == "" {
		return noopThreatScanner{}
	}
	if endpoint == "" {
		endpoint = defaultSafeBrowsingEndpoint
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &safeBrowsingScanner{apiKey: apiKey, endpoint: endpoint, client: client}
}

type safeBrowsingRequest struct {
	Client     safeBrowsingClient     `json:"client"`
	ThreatInfo safeBrowsingThreatInfo `json:"threatInfo"`
}

type safeBrowsingClient struct {
	ClientID      string `json:"clientId"`
	ClientVersion string `json:"clientVersion"`
}

type safeBrowsingThreatInfo struct {
	ThreatTypes      []string            `json:"threatTypes"`
	PlatformTypes    []string            `json:"platformTypes"`
	ThreatEntryTypes []string            `json:"threatEntryTypes"`
	ThreatEntries    []safeBrowsingEntry `json:"threatEntries"`
}

type safeBrowsingEntry struct {
	URL string `json:"url"`
}

type safeBrowsingResponse struct {
	Matches []struct {
		ThreatType string `json:"threatType"`
	} `json:"matches"`
}

func (s *safeBrowsingScanner) Scan(ctx context.Context, rawURL string) (shortlink.ThreatVerdict, error) {
	body := safeBrowsingRequest{
		Client: safeBrowsingClient{ClientID: "vozko", ClientVersion: "1.0.0"},
		ThreatInfo: safeBrowsingThreatInfo{
			ThreatTypes:      []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE", "POTENTIALLY_HARMFUL_APPLICATION"},
			PlatformTypes:    []string{"ANY_PLATFORM"},
			ThreatEntryTypes: []string{"URL"},
			ThreatEntries:    []safeBrowsingEntry{{URL: rawURL}},
		},
	}

	payload, _ := json.Marshal(body)

	url := fmt.Sprintf("%s?key=%s", s.endpoint, s.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return shortlink.ThreatVerdict{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return shortlink.ThreatVerdict{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return shortlink.ThreatVerdict{}, fmt.Errorf("safe browsing returned status %d: %s", resp.StatusCode, string(data))
	}

	var decoded safeBrowsingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return shortlink.ThreatVerdict{}, err
	}

	if len(decoded.Matches) == 0 {
		return shortlink.ThreatVerdict{Safe: true}, nil
	}

	threats := make([]string, 0, len(decoded.Matches))
	for _, m := range decoded.Matches {
		threats = append(threats, m.ThreatType)
	}
	return shortlink.ThreatVerdict{Safe: false, Threats: threats}, nil
}
