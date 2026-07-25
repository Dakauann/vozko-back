package ticket

func (r *GenerateLabelRequest) Validate() map[string]string {
	if r.TrackingCode == "" {
		return map[string]string{"trackingCode": "required"}
	}
	return nil
}
