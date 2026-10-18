package businessphone

import (
	"time"

	"vozko/domain/notification"
)

type PhoneNotice struct {
	State    string
	Severity notification.Severity
	Title    string
	Body     string
	Subject  string
	Template string
	Extra    map[string]interface{}
	Window   time.Duration
}

type PhoneNotifier interface {
	NotifyPhone(phone *WhatsAppBusinessPhoneNumber, notice PhoneNotice) error
}
