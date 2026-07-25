package callrecording

import recordingsdomain "vozko/domain/calls/recordings"

type CallRecordingResponse struct {
	CallID       string  `json:"callId" example:"call_a1b2c3"`
	EntryID      string  `json:"entryId,omitempty" example:"cme_a1b2c3"`
	LeadID       string  `json:"leadId,omitempty" example:"lead_a1b2c3"`
	RecordingURL string  `json:"recordingUrl" example:"https://cdn.exemplo.com/rec/a1b2c3.mp3"`
	DurationSec  int     `json:"durationSec" example:"142"`
	DurationMin  float64 `json:"durationMin" example:"2.37"`
	CallStart    string  `json:"callStart" example:"2026-07-19T14:32:05Z"`
	CallEnd      string  `json:"callEnd" example:"2026-07-19T14:34:27Z"`
	CreatedAt    string  `json:"createdAt" example:"2026-07-19T14:35:00Z"`
}

type CallRecordingListResponse struct {
	Items      []CallRecordingResponse `json:"items"`
	Page       int                     `json:"page" example:"1"`
	PageSize   int                     `json:"pageSize" example:"20"`
	TotalItems int64                   `json:"totalItems" example:"137"`
	TotalPages int                     `json:"totalPages" example:"7"`
}

type CallRecordingCollectionResponse struct {
	Items []CallRecordingResponse `json:"items"`
	Total int                     `json:"total" example:"3"`
}

func toCallRecordingResponse(rec *recordingsdomain.CallRecord) CallRecordingResponse {
	return CallRecordingResponse{
		CallID:       rec.CallID,
		EntryID:      rec.EntryID,
		LeadID:       rec.LeadID,
		RecordingURL: rec.RecordingURL,
		DurationSec:  rec.DurationSec,
		DurationMin:  float64(rec.DurationSec) / 60.0,
		CallStart:    rec.CallStart.Format("2006-01-02T15:04:05Z07:00"),
		CallEnd:      rec.CallEnd.Format("2006-01-02T15:04:05Z07:00"),
		CreatedAt:    rec.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
