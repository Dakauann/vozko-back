package businessphone_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace"
)

const (
	phonesPath          = "/dashboard/whatsapp-business-phones"
	defaultNoticeWindow = 7 * 24 * time.Hour
)

type PhoneNotices struct {
	publisher    notification.Publisher
	dashboardURL string
}

var _ businessphone.PhoneNotifier = (*PhoneNotices)(nil)

func NewPhoneNotices(publisher notification.Publisher, dashboardURL string) *PhoneNotices {
	return &PhoneNotices{publisher: publisher, dashboardURL: strings.TrimRight(dashboardURL, "/")}
}

func (n *PhoneNotices) NotifyPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, notice businessphone.PhoneNotice) error {
	if n == nil || n.publisher == nil || phone == nil || phone.OwnerWorkspaceID == "" {
		return nil
	}
	placeholders := map[string]interface{}{
		"PhoneNumber":  phone.DisplayPhoneNumber,
		"DashboardURL": notification.AbsoluteLink(n.dashboardURL, phonesPath),
	}
	for k, v := range notice.Extra {
		placeholders[k] = v
	}
	window := notice.Window
	if window <= 0 {
		window = defaultNoticeWindow
	}
	body := notice.Body
	if phone.DisplayPhoneNumber != "" {
		body = "Número " + phone.DisplayPhoneNumber + ". " + body
	}
	_, err := n.publisher.Publish(context.Background(), notification.Draft{
		WorkspaceID: phone.OwnerWorkspaceID,
		Category:    notification.CategoryChannels,
		Severity:    notice.Severity,
		Title:       notification.ClipTitle(notice.Title),
		Body:        body,
		ActionURL:   phonesPath,
		ActionLabel: "Ver números",
		Audience:    notification.Holding(workspace.ResourceBusinessPhones, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template:     notice.Template,
			Subject:      notice.Subject + " - " + brand.Active().Name,
			Placeholders: placeholders,
		},
		DedupKey:    "phone_alert:" + notice.State + ":" + phone.ID,
		DedupWindow: window,
		Source:      notification.SourcePhoneAlert,
	})
	return err
}
