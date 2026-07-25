package callbilling

import (
	"time"

	billingdomain "vozko/domain/calls/billing"
)

type CallBillingResponse struct {
	ID          string                      `json:"id" example:"cb_a1b2c3"`
	CallID      string                      `json:"callId" example:"call_a1b2c3"`
	WorkspaceID string                      `json:"workspaceId" example:"ws_a1b2c3"`
	AgentID     *string                     `json:"agentId,omitempty" example:"agt_a1b2c3"`
	LeadID      *string                     `json:"leadId,omitempty" example:"lead_a1b2c3"`
	CallSource  billingdomain.CallSource    `json:"callSource" swaggertype:"string" example:"outbound"`
	Status      billingdomain.BillingStatus `json:"status" swaggertype:"string" example:"charged"`

	CallStart   time.Time `json:"callStart"`
	CallEnd     time.Time `json:"callEnd"`
	DurationSec int       `json:"durationSec" example:"142"`

	TelephonyChargeMicros int64 `json:"telephonyChargeMicros" example:"5000"`
	TotalChargeMicros     int64 `json:"totalChargeMicros" example:"12000"`

	TransactionID *string `json:"transactionId,omitempty" example:"txn_a1b2c3"`
	RecordingURL  *string `json:"recordingUrl,omitempty" example:"https://cdn.example.com/rec/a1b2c3.mp3"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CallBillingListResponse struct {
	Items      []CallBillingResponse `json:"items"`
	Page       int                   `json:"page" example:"1"`
	PageSize   int                   `json:"page_size" example:"20"`
	TotalItems int64                 `json:"total_items" example:"137"`
	TotalPages int                   `json:"total_pages" example:"7"`
}

func ToCallBillingResponse(r *billingdomain.CallBillingRecord) CallBillingResponse {
	return CallBillingResponse{
		ID:          r.ID,
		CallID:      r.CallID,
		WorkspaceID: r.WorkspaceID,
		AgentID:     r.AgentID,
		LeadID:      r.LeadID,
		CallSource:  r.CallSource,
		Status:      r.Status,

		CallStart:   r.CallStart,
		CallEnd:     r.CallEnd,
		DurationSec: r.DurationSec,

		TelephonyChargeMicros: r.TelephonyRevenueMicros,
		TotalChargeMicros:     r.TotalRevenueMicros,

		TransactionID: r.TransactionID,
		RecordingURL:  r.RecordingURL,

		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
