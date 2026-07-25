package export

func (r ExportEntriesRequest) Validate() map[string]string {
	if r.CampaignID == "" {
		return map[string]string{"id": "required"}
	}
	return nil
}
