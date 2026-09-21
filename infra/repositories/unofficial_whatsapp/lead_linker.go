package unofficial_whatsapp_repository

import (
	"context"
	"strings"

	lead_domain "vozko/domain/lead"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type leadLinker struct {
	repo lead_domain.Repository
}

func NewLeadLinker(repo lead_domain.Repository) uwuc.LeadLinker {
	return &leadLinker{repo: repo}
}

func (l *leadLinker) EnsureLeadForPhone(_ context.Context, workspaceID, phone, name string) (string, error) {
	if l.repo == nil || workspaceID == "" {
		return "", nil
	}

	normalized := lead_domain.NormalizeNumber(phone)
	if normalized == "" {
		return "", nil
	}

	if existing, err := l.repo.FindByNumber(workspaceID, normalized); err == nil &&
		existing != nil && strings.TrimSpace(existing.Name) != "" {
		return existing.ID, nil
	}

	record, _, err := l.repo.FindOrCreate(workspaceID, normalized, lead_domain.LeadUpdate{Name: name})
	if err != nil || record == nil {
		return "", err
	}
	return record.ID, nil
}
