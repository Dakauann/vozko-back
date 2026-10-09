package workspaceconfig

type UpdateGeocodingSettingsRequest struct {
	Provider       *string `json:"provider,omitempty" example:"opencage"`
	MonthlyCeiling *int64  `json:"monthlyCeiling,omitempty" example:"5000"`
}

type GeocodingSettingsResponse struct {
	Provider              string                          `json:"provider" example:"opencage"`
	Enabled               bool                            `json:"enabled" example:"true"`
	ProviderChangedBy     string                          `json:"providerChangedBy,omitempty" example:"5f0c2b1e-7a1d-4c2b-9e1f-0a1b2c3d4e5f"`
	ProviderChangedByName string                          `json:"providerChangedByName,omitempty" example:"Ana Souza"`
	ProviderChangedAt     *string                         `json:"providerChangedAt,omitempty" example:"2026-10-08T12:00:00Z"`
	MonthlyCeiling        int64                           `json:"monthlyCeiling" example:"5000"`
	CeilingChangedBy      string                          `json:"ceilingChangedBy,omitempty" example:"5f0c2b1e-7a1d-4c2b-9e1f-0a1b2c3d4e5f"`
	CeilingChangedByName  string                          `json:"ceilingChangedByName,omitempty" example:"Suporte Vozko"`
	CeilingChangedAt      *string                         `json:"ceilingChangedAt,omitempty" example:"2026-10-08T12:00:00Z"`
	DailyShare            int64                           `json:"dailyShare" example:"323"`
	UsedThisCycle         int64                           `json:"usedThisCycle" example:"120"`
	UsedToday             int64                           `json:"usedToday" example:"12"`
	Exhausted             string                          `json:"exhausted" example:"" enums:",monthly,daily"`
	CycleStart            *string                         `json:"cycleStart,omitempty" example:"2026-10-01T03:00:00Z"`
	NextCycleStart        *string                         `json:"nextCycleStart,omitempty" example:"2026-11-01T03:00:00Z"`
	AvailableProviders    []string                        `json:"availableProviders"`
	Attribution           string                          `json:"attribution" example:"IBGE, CNEFE 2022"`
	CanChangeProvider     bool                            `json:"canChangeProvider" example:"true"`
	CanChangeCeiling      bool                            `json:"canChangeCeiling" example:"false"`
	ProviderPause         *GeocodingProviderPauseResponse `json:"providerPause"`
}

type GeocodingProviderPauseResponse struct {
	State  string  `json:"state" example:"paused" enums:"paused,unknown"`
	Reason string  `json:"reason,omitempty" example:"key_rejected" enums:"key_rejected,account_quota_spent,key_disabled,account_refused,queries_refused"`
	Since  *string `json:"since,omitempty" example:"2026-10-08T15:00:00Z"`
	Until  *string `json:"until,omitempty" example:"2026-10-08T16:00:00Z"`
}
