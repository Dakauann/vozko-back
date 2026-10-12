package openrouter

import (
	"testing"

	"vozko/infra/config"
)

func TestTheServiceConfigComesFromTheApplicationConfigAlone(t *testing.T) {
	got := ConfigFrom(config.Config{OpenRouterAPIKey: "k", OpenRouterDefaultModel: "m", OpenRouterHTTPReferer: "r", OpenRouterXTitle: "x"})
	if got != (Config{APIKey: "k", DefaultModel: "m", HTTPReferer: "r", XTitle: "x"}) {
		t.Fatalf("config = %+v", got)
	}
}
