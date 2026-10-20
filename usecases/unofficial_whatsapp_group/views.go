package unofficial_whatsapp_group_usecase

import (
	"encoding/json"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/unofficial_whatsapp_group"
)

type DiscoverResult struct {
	Numbers int      `json:"numbers"`
	Groups  int      `json:"groups"`
	Failed  []string `json:"failed"`
}

type NumberSeat struct {
	InstanceID string `json:"instanceId"`
	Label      string `json:"label"`
	Phone      string `json:"phone,omitempty"`
	Connected  bool   `json:"connected"`
	Admin      bool   `json:"admin"`
	CanPost    bool   `json:"canPost"`
}

type GroupSummary struct {
	Sentiment    unofficial_whatsapp_group.RunSentiment   `json:"sentiment"`
	Positivity   int                                      `json:"positivity"`
	Cycles       int                                      `json:"cycles"`
	Messages     int                                      `json:"messages"`
	HighAlertPct int                                      `json:"highAlertPct"`
	Themes       []unofficial_whatsapp_group.LabelCount   `json:"themes"`
	Mentions     []unofficial_whatsapp_group.MentionTotal `json:"mentions"`
	LastRunAt    *time.Time                               `json:"lastRunAt,omitempty"`
}

type GroupRow struct {
	Group            *unofficial_whatsapp_group.Group        `json:"group"`
	GroupCreatedAt   *time.Time                              `json:"groupCreatedAt,omitempty"`
	Numbers          []NumberSeat                            `json:"numbers"`
	Activity         unofficial_whatsapp_group.GroupActivity `json:"activity"`
	Summary          *GroupSummary                           `json:"summary,omitempty"`
	HasAnalysis      bool                                    `json:"hasAnalysis"`
	RunningCampaigns int                                     `json:"runningCampaigns"`
}

type GroupDetail struct {
	GroupRow
	ConversationID string                                   `json:"conversationId,omitempty"`
	Members        unofficial_whatsapp_group.MemberCounts   `json:"members"`
	Growth         []unofficial_whatsapp_group.DailyPoint   `json:"growth"`
	Overview       unofficial_whatsapp_group.Overview       `json:"overview"`
	LatestRun      *unofficial_whatsapp_group.Run           `json:"latestRun,omitempty"`
	Analyses       []*unofficial_whatsapp_group.AnalysisRow `json:"analyses"`
	Series         []SeriesSibling                          `json:"series"`
}

type SeriesSibling struct {
	GroupID  string `json:"groupId"`
	Subject  string `json:"subject"`
	Sequence int    `json:"sequence"`
	Members  int    `json:"members"`
}

type Connections struct {
	Connected int `json:"connected"`
	Total     int `json:"total"`
}

type AnalysisTotals struct {
	Total  int64 `json:"total"`
	Active int64 `json:"active"`
}

type DashboardStats struct {
	Connections      Connections                                   `json:"connections"`
	Groups           unofficial_whatsapp_group.GroupTotals         `json:"groups"`
	People           unofficial_whatsapp_group.PeopleTotals        `json:"people"`
	Analyses         AnalysisTotals                                `json:"analyses"`
	Runs             unofficial_whatsapp_group.RunTotals           `json:"runs"`
	DailyVolume      []unofficial_whatsapp_group.DailyPoint        `json:"dailyVolume"`
	TopGroups        []unofficial_whatsapp_group.GroupCount        `json:"topGroups"`
	Sentiment        unofficial_whatsapp_group.SentimentTally      `json:"sentiment"`
	SentimentByGroup []unofficial_whatsapp_group.GroupSentiment    `json:"sentimentByGroup"`
	ResponseTime     []unofficial_whatsapp_group.ResponseTimePoint `json:"responseTime"`
	Growth           []unofficial_whatsapp_group.DailyPoint        `json:"growth"`
	TopGrowing       []unofficial_whatsapp_group.GroupGrowth       `json:"topGrowing"`
	Insights         []unofficial_whatsapp_group.Insight           `json:"insights"`
	CrossGroupPeople []*unofficial_whatsapp_group.Person           `json:"crossGroupPeople"`
	CrossGroupTotal  int64                                         `json:"crossGroupTotal"`
	Themes           []*unofficial_whatsapp_group.ThemeAggregate   `json:"themes"`
	Timezone         string                                        `json:"timezone"`
}

type AnalysisDetail struct {
	Analysis          *unofficial_whatsapp_group.Analysis             `json:"analysis"`
	Group             *unofficial_whatsapp_group.Group                `json:"group"`
	Overview          unofficial_whatsapp_group.Overview              `json:"overview"`
	ParticipantGroups map[string][]unofficial_whatsapp_group.GroupRef `json:"participantGroups"`
	WindowDays        int                                             `json:"windowDays"`
	Timezone          string                                          `json:"timezone"`
}

type ManagedGroupResult struct {
	Group    *unofficial_whatsapp_group.Group         `json:"group"`
	Warnings []unofficial_whatsapp_group.SetupWarning `json:"warnings"`
}

type InvitePreview struct {
	InviteLink  string   `json:"inviteLink"`
	JID         string   `json:"jid"`
	Subject     string   `json:"subject"`
	Description string   `json:"description,omitempty"`
	Members     int      `json:"members"`
	Announce    bool     `json:"announce"`
	AlreadyIn   []string `json:"alreadyIn"`
}

type JoinResult struct {
	PlanID string                               `json:"planId"`
	Group  *unofficial_whatsapp_group.Group     `json:"group,omitempty"`
	Tasks  []unofficial_whatsapp_group.JoinTask `json:"tasks"`
}

type LeadLinkResult struct {
	Linked        int `json:"linked"`
	AlreadyLinked int `json:"alreadyLinked"`
	WithoutPhone  int `json:"withoutPhone"`
}

type GroupMessage struct {
	ID               string                               `json:"id"`
	EntryID          string                               `json:"entryId"`
	Channel          conversation.MessageChannel          `json:"channel"`
	MessageType      conversation.MessageType             `json:"messageType"`
	Direction        conversation.MessageHistoryDirection `json:"direction"`
	From             string                               `json:"from"`
	SenderName       string                               `json:"senderName"`
	SenderAvatar     string                               `json:"senderAvatar,omitempty"`
	OurTeam          bool                                 `json:"ourTeam"`
	Text             string                               `json:"text"`
	Transcript       string                               `json:"transcript,omitempty"`
	MediaID          *string                              `json:"mediaId,omitempty"`
	MediaType        conversation.MediaType               `json:"mediaType,omitempty"`
	Media            *conversation.AttachedMedia          `json:"media,omitempty"`
	ReplyToMessageID *string                              `json:"replyToMessageId,omitempty"`
	DeliveryStatus   conversation.DeliveryStatus          `json:"deliveryStatus,omitempty"`
	Read             bool                                 `json:"read"`
	Metadata         json.RawMessage                      `json:"metadata,omitempty" swaggertype:"object"`
	EditedAt         *time.Time                           `json:"editedAt,omitempty"`
	RetractedAt      *time.Time                           `json:"retractedAt,omitempty"`
	CreatedAt        time.Time                            `json:"createdAt"`
}

type GroupMessagesPage struct {
	ConversationID string         `json:"conversationId,omitempty"`
	Messages       []GroupMessage `json:"messages"`
	HasMore        bool           `json:"hasMore"`
	NextBefore     *time.Time     `json:"nextBefore,omitempty"`
}
