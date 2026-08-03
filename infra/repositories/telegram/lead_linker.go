package telegram_repository

import (
	"context"

	lead_domain "vozko/domain/lead"
	tguc "vozko/usecases/telegram"
)

// leadLinker resolves a consented phone share to an existing CRM lead.
//
// This is the one moment a Telegram contact can be bridged to the rest of the
// CRM. Telegram never volunteers a phone number, it arrives only when the
// customer taps a request_contact button, so without this the contact's LeadID
// column could never be populated by anything, and the same human would exist
// twice: once as a WhatsApp lead and once as an unlinked Telegram contact.
//
// It lives here rather than in the container because it is a repository query
// plus a normalization rule, not composition. The number goes through the same
// Brazilian normalisation `leads` is keyed on (ux_leads_workspace_number, which
// stores the 9th-digit-normalised form); comparing the raw share against it
// would miss a perfectly valid match.
type leadLinker struct {
	repo lead_domain.Repository
}

// NewLeadLinker builds the phone-share → lead bridge.
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
		// A miss is not an error. Most Telegram contacts have no CRM lead, and
		// failing the inbound message because of that would redeliver a message
		// that was already stored.
		return "", nil
	}
	return record.ID, nil
}
