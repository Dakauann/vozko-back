package unofficial_whatsapp_repository

import (
	"context"

	lead_domain "vozko/domain/lead"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

// leadLinker bridges a contact to a CRM lead.
//
// This is the decisive difference between this channel and the other two added
// recently. An Instagram IGSID and a Telegram user id are opaque identifiers no
// other subsystem can address, so their contacts stay a parallel address book.
// Here the contact IS an E.164 number — the same key `leads` is already indexed
// on — so the dialer, boletos, opportunities, campaigns and export all reach the
// same person the inbox shows.
//
// It find-or-CREATES rather than only looking up, unlike Telegram's, and the
// difference is deliberate: a Telegram phone share is a rare consent event where
// a miss is normal, while here every inbound message carries a real number, and
// a customer who messages a connected line and does not become a lead is a hole
// in the CRM.
//
// It lives here rather than in the container because it is a repository query
// plus a normalization rule, not composition. The number goes through the same
// Brazilian normalisation `leads` is keyed on (ux_leads_workspace_number stores
// the 9th-digit-normalised form); comparing a raw JID-derived number against it
// would miss a perfectly valid match and duplicate the lead.
type leadLinker struct {
	repo lead_domain.Repository
}

// NewLeadLinker builds the contact → lead bridge.
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

	// FindOrCreate rather than Create: two messages arriving together from the
	// same new number would otherwise race and one would fail on the workspace
	// uniqueness index, losing a message for a reason the customer never sees.
	record, _, err := l.repo.FindOrCreate(workspaceID, normalized, lead_domain.LeadUpdate{Name: name})
	if err != nil || record == nil {
		// A failure here is not fatal to the message. The contact still renders
		// through the identity lookup, and the next inbound message retries the
		// bridge — whereas failing the whole delivery would redeliver a message
		// that was already stored.
		return "", err
	}
	return record.ID, nil
}
