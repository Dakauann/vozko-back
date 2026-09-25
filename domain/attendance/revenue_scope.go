package attendance

type RevenueScope struct {
	CampaignID   string
	CampaignType string
	Channel      string
	DepartmentID string
}

func (s RevenueScope) Empty() bool {
	return s.CampaignID == "" && s.Channel == "" && s.DepartmentID == ""
}

func (f OverviewFilter) RevenueScope() RevenueScope {
	return RevenueScope{
		CampaignID:   f.CampaignID,
		CampaignType: f.CampaignType,
		Channel:      f.Channel,
		DepartmentID: f.DepartmentID,
	}
}
