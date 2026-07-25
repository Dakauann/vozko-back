package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const (
	publishInterval = 10 * time.Second
	publishTTL      = 30
	httpTimeout     = 5 * time.Second
)

type Publisher struct {
	AccountID   string
	NamespaceID string
	APIToken    string
	KeyPrefix   string
	ReplicaID   string
	PublicURL   string
	Region      string

	httpClient *http.Client
}

func New(accountID, namespaceID, apiToken, keyPrefix, replicaID, publicURL, region string) *Publisher {
	if accountID == "" || namespaceID == "" || apiToken == "" || replicaID == "" || publicURL == "" {
		return nil
	}
	if keyPrefix == "" {
		keyPrefix = "replicas:"
	}
	return &Publisher{
		AccountID:   accountID,
		NamespaceID: namespaceID,
		APIToken:    apiToken,
		KeyPrefix:   keyPrefix,
		ReplicaID:   replicaID,
		PublicURL:   publicURL,
		Region:      region,
		httpClient:  &http.Client{Timeout: httpTimeout},
	}
}

type entry struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Region    string `json:"region,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

func (p *Publisher) Run(ctx context.Context) {
	if p == nil {
		return
	}
	log.Printf("[cf-kv] publisher started (replica=%s url=%s prefix=%s)", p.ReplicaID, p.PublicURL, p.KeyPrefix)

	if err := p.publish(ctx); err != nil {
		log.Printf("[cf-kv] initial publish failed: %v", err)
	}

	ticker := time.NewTicker(publishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.delete()
			return
		case <-ticker.C:
			if err := p.publish(ctx); err != nil {
				log.Printf("[cf-kv] publish failed: %v", err)
			}
		}
	}
}

func (p *Publisher) publish(ctx context.Context) error {
	body, err := json.Marshal(entry{
		ID:        p.ReplicaID,
		URL:       p.PublicURL,
		Region:    p.Region,
		UpdatedAt: time.Now().Unix(),
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/values/%s%s?expiration_ttl=60",
		p.AccountID, p.NamespaceID, p.KeyPrefix, p.ReplicaID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("KV PUT %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

func (p *Publisher) delete() {
	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/storage/kv/namespaces/%s/values/%s%s",
		p.AccountID, p.NamespaceID, p.KeyPrefix, p.ReplicaID,
	)
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.APIToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		log.Printf("[cf-kv] graceful delete failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[cf-kv] entry deleted on shutdown")
}
