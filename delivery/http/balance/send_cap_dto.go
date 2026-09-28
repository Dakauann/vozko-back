package balance

import (
	"time"

	balancedomain "vozko/domain/balance"
)

type SetSendCapRequest struct {
	Limit *int64 `json:"limit" example:"5000"`
}

type UnlockSendCapRequest struct {
	Limit     *int64 `json:"limit,omitempty" example:"20000"`
	RemoveCap bool   `json:"removeCap,omitempty" example:"false"`
	Code      string `json:"code" example:"0000"`
}

type SendCapItemResponse struct {
	WorkspaceID   string  `json:"workspaceId"`
	WorkspaceName string  `json:"workspaceName"`
	Limit         int64   `json:"limit"`
	Used          int64   `json:"used"`
	Remaining     int64   `json:"remaining"`
	Level         string  `json:"level" enums:"ok,near,reached"`
	UpdatedBy     string  `json:"updatedBy"`
	UpdatedAt     string  `json:"updatedAt"`
	UnlockedBy    *string `json:"unlockedBy,omitempty"`
	UnlockedAt    *string `json:"unlockedAt,omitempty"`
}

type SendCapListResponse struct {
	MonthStart string                `json:"monthStart"`
	CanUnlock  bool                  `json:"canUnlock"`
	Items      []SendCapItemResponse `json:"items"`
}

type SendCapChangeResponse struct {
	WorkspaceID string `json:"workspaceId"`
	Limit       *int64 `json:"limit"`
}

func toSendCapListResponse(listing *balancedomain.SendCapListing) SendCapListResponse {
	items := make([]SendCapItemResponse, 0, len(listing.Items))
	for _, usage := range listing.Items {
		items = append(items, SendCapItemResponse{
			WorkspaceID:   usage.Cap.WorkspaceID,
			WorkspaceName: usage.WorkspaceName,
			Limit:         usage.Cap.Limit,
			Used:          usage.Used,
			Remaining:     usage.Remaining(),
			Level:         string(usage.Level()),
			UpdatedBy:     usage.Cap.UpdatedBy,
			UpdatedAt:     usage.Cap.UpdatedAt.Format(time.RFC3339),
			UnlockedBy:    usage.Cap.UnlockedBy,
			UnlockedAt:    formatOptionalTime(usage.Cap.UnlockedAt),
		})
	}
	return SendCapListResponse{
		MonthStart: listing.MonthStart.Format(time.RFC3339),
		CanUnlock:  listing.CanUnlock,
		Items:      items,
	}
}

func toSendCapChangeResponse(workspaceID string, cap *balancedomain.MonthlySendCap) SendCapChangeResponse {
	if cap == nil {
		return SendCapChangeResponse{WorkspaceID: workspaceID}
	}
	limit := cap.Limit
	return SendCapChangeResponse{WorkspaceID: workspaceID, Limit: &limit}
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.Format(time.RFC3339)
	return &formatted
}
