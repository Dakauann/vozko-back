package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"vozko/infra/cmcom"
)

const defaultAddr = "127.0.0.1:4810"

type config struct {
	addr         string
	productToken string
	accountID    string
	webhookBase  string
}

func loadConfig(getenv func(string) string) (config, error) {
	cfg := config{
		addr:         strings.TrimSpace(getenv("CMFAKE_ADDR")),
		productToken: strings.TrimSpace(getenv("CMFAKE_PRODUCT_TOKEN")),
		accountID:    strings.TrimSpace(getenv("CMFAKE_ACCOUNT_ID")),
		webhookBase:  strings.TrimRight(strings.TrimSpace(getenv("CMFAKE_WEBHOOK_BASE")), "/"),
	}
	if cfg.addr == "" {
		cfg.addr = defaultAddr
	}
	if _, _, err := net.SplitHostPort(cfg.addr); err != nil {
		return config{}, fmt.Errorf("CMFAKE_ADDR must be host:port: %w", err)
	}
	if err := (cmcom.Config{ProductToken: cfg.productToken, AccountID: cfg.accountID}).Validate(); err != nil {
		return config{}, fmt.Errorf("CMFAKE_PRODUCT_TOKEN and CMFAKE_ACCOUNT_ID: %w", err)
	}
	if cfg.webhookBase == "" {
		return config{}, errors.New("CMFAKE_WEBHOOK_BASE is required")
	}
	u, err := url.Parse(cfg.webhookBase)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return config{}, errors.New("CMFAKE_WEBHOOK_BASE must be the backend webhook address, such as http://127.0.0.1:4000/webhooks/carrier/cmcom/<token>")
	}
	return cfg, nil
}
