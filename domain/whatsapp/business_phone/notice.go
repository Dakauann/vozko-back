package businessphone

import "vozko/domain/notification"

type PhoneNotice struct {
	State    string
	Severity notification.Severity
	Title    string
	Body     string
	Subject  string
	Template string
	Extra    map[string]interface{}
	Ends     []string
}

type PhoneNotifier interface {
	NotifyPhone(phone *WhatsAppBusinessPhoneNumber, notice PhoneNotice) error
	ClearPhone(phone *WhatsAppBusinessPhoneNumber, states ...string) error
}

func (n PhoneNotice) Persists() bool {
	return n.Severity == notification.SeverityWarning || n.Severity == notification.SeverityCritical
}
