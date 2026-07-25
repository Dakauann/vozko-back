package calendar

func (r *CreateEventRequest) Validate() map[string]string {
	if r.Title == "" || r.StartTime == "" || r.EndTime == "" {
		expected := make(map[string]string)
		if r.Title == "" {
			expected["title"] = "required"
		}
		if r.StartTime == "" {
			expected["startTime"] = "required"
		}
		if r.EndTime == "" {
			expected["endTime"] = "required"
		}
		return expected
	}
	return nil
}
