package lead_message_window

import (
	"errors"
	"time"
)

const MessageWindowDuration = 24 * time.Hour

var (
	ErrWindowNotFound         = errors.New("lead message window: not found")
	ErrLeadIDRequired         = errors.New("lead message window: lead id is required")
	ErrBusinessPhoneIDRequied = errors.New("lead message window: business phone id is required")
)

type LeadMessageWindow struct {
	ID              string    `json:"id"`
	LeadID          string    `json:"leadId"`
	BusinessPhoneID string    `json:"businessPhoneId"`
	LastMessageAt   time.Time `json:"lastMessageAt"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func (w *LeadMessageWindow) IsWindowOpen() bool {
	return time.Since(w.LastMessageAt) < MessageWindowDuration
}

func (w *LeadMessageWindow) WindowExpiresAt() time.Time {
	return w.LastMessageAt.Add(MessageWindowDuration)
}

type WindowStatus struct {
	BusinessPhoneID          string     `json:"businessPhoneId"`
	LastMessageAt            *time.Time `json:"lastMessageAt,omitempty"`
	CanReceiveNormalMessages bool       `json:"canReceiveNormalMessages"`
	WindowExpiresAt          *time.Time `json:"windowExpiresAt,omitempty"`
}

func NewWindowStatus(window *LeadMessageWindow, businessPhoneID string) WindowStatus {
	if window == nil {
		return WindowStatus{
			BusinessPhoneID:          businessPhoneID,
			CanReceiveNormalMessages: false,
		}
	}

	isOpen := window.IsWindowOpen()
	status := WindowStatus{
		BusinessPhoneID:          window.BusinessPhoneID,
		LastMessageAt:            &window.LastMessageAt,
		CanReceiveNormalMessages: isOpen,
	}

	if isOpen {
		expiresAt := window.WindowExpiresAt()
		status.WindowExpiresAt = &expiresAt
	}

	return status
}
