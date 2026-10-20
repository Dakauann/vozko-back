package lead_message_window

import "time"

type Repository interface {
	RecordMessage(leadID, businessPhoneID string) (*LeadMessageWindow, error)

	FindByLeadAndBusinessPhone(leadID, businessPhoneID string) (*LeadMessageWindow, error)

	FindAllByLead(leadID string) ([]*LeadMessageWindow, error)

	FindByLeadIDs(leadIDs []string) ([]*LeadMessageWindow, error)

	IsWindowOpen(leadID, businessPhoneID string) (bool, error)

	GetLastMessageTime(leadID, businessPhoneID string) (*time.Time, error)
}
