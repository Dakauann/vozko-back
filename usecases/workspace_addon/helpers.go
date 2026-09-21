package workspace_addon_usecase

import (
	"context"
	"time"

	wsc "vozko/domain/workspace_config"
)

type clockFn func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

const balanceCurrencyUSD = "USD"

type includedInstanceReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type batchIncludedInstanceReader interface {
	GetIncludedUnofficialInstancesByWorkspaceIDs(ctx context.Context, workspaceIDs []string) (map[string]int, error)
}

func readIncludedInstances(configs includedInstanceReader, workspaceID string) (int, error) {
	if configs == nil {
		return 0, nil
	}
	cfg, err := configs.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil {
		return 0, err
	}
	if cfg == nil {
		return 0, nil
	}
	return cfg.IncludedUnofficialWhatsAppInstances, nil
}
