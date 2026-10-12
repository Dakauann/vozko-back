package openrouter

import "vozko/infra/config"

func ConfigFrom(cfg config.Config) Config {
	return Config{APIKey: cfg.OpenRouterAPIKey, DefaultModel: cfg.OpenRouterDefaultModel, HTTPReferer: cfg.OpenRouterHTTPReferer, XTitle: cfg.OpenRouterXTitle}
}
