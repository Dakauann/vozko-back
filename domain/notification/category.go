package notification

type Category string

const (
	CategoryAnnouncements Category = "announcements"
	CategoryCampaigns     Category = "campaigns"
	CategoryTemplates     Category = "templates"
	CategoryChannels      Category = "channels"
	CategoryBilling       Category = "billing"
	CategoryAdvertising   Category = "advertising"
	CategoryTeam          Category = "team"
	CategorySupport       Category = "support"
	CategorySystem        Category = "system"
)

var categoryCatalog = []Category{
	CategoryAnnouncements,
	CategoryCampaigns,
	CategoryTemplates,
	CategoryChannels,
	CategoryBilling,
	CategoryAdvertising,
	CategoryTeam,
	CategorySupport,
	CategorySystem,
}

func Categories() []Category {
	out := make([]Category, len(categoryCatalog))
	copy(out, categoryCatalog)
	return out
}

func (c Category) Valid() bool {
	for _, known := range categoryCatalog {
		if c == known {
			return true
		}
	}
	return false
}

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeveritySuccess  Severity = "success"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeveritySuccess, SeverityWarning, SeverityCritical:
		return true
	}
	return false
}
