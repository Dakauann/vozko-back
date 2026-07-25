package ticket

type UpdateStatusRequest struct {
	Status       string  `json:"status" example:"ready_for_label"`
	TrackingCode *string `json:"trackingCode,omitempty" example:"BR123456789BR"`
}

type GenerateLabelRequest struct {
	TrackingCode string `json:"trackingCode" example:"BR123456789BR"`
}
