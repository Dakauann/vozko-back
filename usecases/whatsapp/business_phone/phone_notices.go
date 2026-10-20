package businessphone_usecase

import (
	"context"
	"strings"

	"vozko/brand"
	"vozko/domain/notification"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace"
)

const phonesPath = "/dashboard/whatsapp-business-phones"

type PhoneNotices struct {
	publisher    notification.EpisodePublisher
	dashboardURL string
}

var _ businessphone.PhoneNotifier = (*PhoneNotices)(nil)

func NewPhoneNotices(publisher notification.EpisodePublisher, dashboardURL string) *PhoneNotices {
	return &PhoneNotices{publisher: publisher, dashboardURL: strings.TrimRight(dashboardURL, "/")}
}

func (n *PhoneNotices) NotifyPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, notice businessphone.PhoneNotice) error {
	if !n.owned(phone) {
		return nil
	}
	if err := n.ClearPhone(phone, notice.Ends...); err != nil {
		return err
	}
	placeholders := map[string]interface{}{
		"PhoneNumber":  phone.DisplayPhoneNumber,
		"DashboardURL": notification.AbsoluteLink(n.dashboardURL, phonesPath),
	}
	for k, v := range notice.Extra {
		placeholders[k] = v
	}
	episodeKey := ""
	if notice.Persists() {
		episodeKey = phoneEpisodeKey(notice.State, phone.ID)
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
		EpisodeKey: episodeKey,
		Source:     notification.SourcePhoneAlert,
	})
	return err
}

func (n *PhoneNotices) ClearPhone(phone *businessphone.WhatsAppBusinessPhoneNumber, states ...string) error {
	if !n.owned(phone) || len(states) == 0 {
		return nil
	}
	keys := make([]string, 0, len(states))
	for _, state := range states {
		keys = append(keys, phoneEpisodeKey(state, phone.ID))
	}
	return n.publisher.Resolve(context.Background(), phone.OwnerWorkspaceID, keys...)
}

func (n *PhoneNotices) owned(phone *businessphone.WhatsAppBusinessPhoneNumber) bool {
	return n != nil && n.publisher != nil && phone != nil && phone.OwnerWorkspaceID != ""
}

func phoneEpisodeKey(state, phoneID string) string {
	return "phone_alert:" + state + ":" + phoneID
}
