package emailsending

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	es "vozko/domain/emailsending"
)

const (
	tenantHealthPrefix = "email:tenant_health:"
	tenantHealthTTL    = 72 * time.Hour
)

type tenantHealthSnapshot struct {
	Health    *es.TenantHealth
	Error     string
	CheckedAt time.Time
}

type TenantHealthSweep struct {
	c *core
}

func (s *TenantHealthSweep) Execute(ctx context.Context) error {
	c := s.c
	ctx = es.WithProviderLane(ctx, es.LaneBackground)
	accounts, err := c.allAccounts(ctx)
	if err != nil {
		return err
	}
	failures := 0
	for i := range accounts {
		if accounts[i].Status == es.AccountDormant {
			continue
		}
		if _, err := c.checkHealth(ctx, &accounts[i]); err != nil {
			failures++
			log.Printf("[emailsending] health of tenant %s unavailable: %v", accounts[i].TenantName, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("emailsending: %d of %d tenant health checks failed", failures, len(accounts))
	}
	return nil
}

func (c *core) checkHealth(ctx context.Context, acct *es.SenderAccount) (es.TenantHealth, error) {
	health, err := c.Provider.TenantHealth(ctx, acct.TenantName)
	snapshot := tenantHealthSnapshot{CheckedAt: c.Now()}
	if err != nil {
		err = providerFailure("tenant health", err)
		snapshot.Error = err.Error()
	} else {
		snapshot.Health = &health
	}
	c.storeHealth(acct.TenantName, snapshot)
	return health, err
}

func (c *core) storeHealth(tenantName string, snapshot tenantHealthSnapshot) {
	raw, err := json.Marshal(snapshot)
	if err == nil {
		err = c.Shared.SetString(tenantHealthPrefix+tenantName, string(raw), tenantHealthTTL)
	}
	if err != nil {
		log.Printf("[emailsending] health of tenant %s not cached for the admin list: %v", tenantName, err)
	}
}

func (c *core) cachedHealth(tenantName string) (tenantHealthSnapshot, bool) {
	raw, err := c.Shared.GetString(tenantHealthPrefix + tenantName)
	if err != nil {
		return tenantHealthSnapshot{Error: "cached health unreadable: " + err.Error()}, true
	}
	if strings.TrimSpace(raw) == "" {
		return tenantHealthSnapshot{}, false
	}
	var snapshot tenantHealthSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return tenantHealthSnapshot{Error: "cached health undecodable: " + err.Error()}, true
	}
	return snapshot, true
}
