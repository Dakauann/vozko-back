package config_usecase

import (
	"context"

	"vozko/domain/config"
)

type getSystemConfigUseCase struct {
	repo config.SystemConfigRepository
}

func NewGetSystemConfigUseCase(repo config.SystemConfigRepository) config.GetSystemConfigUseCase {
	return &getSystemConfigUseCase{repo: repo}
}

func (uc *getSystemConfigUseCase) Execute(ctx context.Context) (*config.SystemConfig, error) {
	return uc.repo.Get(ctx)
}
