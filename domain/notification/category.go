package notification

type Category string

const (
	CategoryAnnouncements Category = "announcements"
	CategoryCampaigns     Category = "campaigns"
	CategoryConversations Category = "conversations"
	CategoryTemplates     Category = "templates"
	CategoryChannels      Category = "channels"
	CategoryBilling       Category = "billing"
	CategoryAdvertising   Category = "advertising"
	CategoryTeam          Category = "team"
	CategorySupport       Category = "support"
	CategorySystem        Category = "system"
	CategoryAutomations   Category = "automations"
)

var categoryCatalog = []Category{
	CategoryAnnouncements,
	CategoryCampaigns,
	CategoryConversations,
	CategoryTemplates,
	CategoryChannels,
	CategoryBilling,
	CategoryAdvertising,
	CategoryTeam,
	CategorySupport,
	CategorySystem,
	CategoryAutomations,
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
