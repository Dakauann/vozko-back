package carriermessaging_usecase

import (
	"context"
	"strings"

	"vozko/domain/config"
	wsc "vozko/domain/workspace_config"
)

type WorkspaceConfigs interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type workspaceTimezones struct {
	configs WorkspaceConfigs
}

func NewWorkspaceTimezones(configs WorkspaceConfigs) WorkspaceTimezones {
	return &workspaceTimezones{configs: configs}
}

func (w *workspaceTimezones) Timezone(ctx context.Context, workspaceID string) (string, error) {
	cfg, err := w.configs.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if cfg != nil && cfg.WorkingHours != nil && strings.TrimSpace(cfg.WorkingHours.Timezone) != "" {
		return cfg.WorkingHours.Timezone, nil
	}
	return config.BrazilianTimezone.String(), nil
}
