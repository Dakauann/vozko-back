package telegram_repository

import (
	"context"

	lead_domain "vozko/domain/lead"
	tguc "vozko/usecases/telegram"
)

type leadLinker struct {
	repo lead_domain.Repository
}

func NewLeadLinker(repo lead_domain.Repository) tguc.LeadLinker {
	return &leadLinker{repo: repo}
}

func (l *leadLinker) FindLeadIDByPhone(_ context.Context, workspaceID, phone string) (string, error) {
	if l.repo == nil || workspaceID == "" {
		return "", nil
	}

	normalized := lead_domain.NormalizeNumber(phone)
	if normalized == "" {
		return "", nil
	}

	record, err := l.repo.FindByNumber(workspaceID, normalized)
	if err != nil || record == nil {
		return "", nil
	}
	return record.ID, nil
}
