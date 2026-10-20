package unofficial_whatsapp_group

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type GroupSort string

const (
	SortRecent   GroupSort = "recent"
	SortOldest   GroupSort = "oldest"
	SortName     GroupSort = "name"
	SortNameDesc GroupSort = "name_desc"
	SortMembers  GroupSort = "members"
)

type GroupFilter struct {
	WorkspaceID string
	Scope       Scope
	Search      string
	Followed    *bool
	Origin      Origin
	Category    *string
	InstanceID  string
	AdminsOnly  *bool
	HasAnalysis *bool
	SeriesID    string
	Sort        GroupSort
	Pagination  shared.Pagination
}

type GroupRepository interface {
	UpsertSeen(ctx context.Context, g *Group) error
	Save(ctx context.Context, g *Group) error
	SetFollowed(ctx context.Context, workspaceID, id string, at *time.Time) error
	SetPrimary(ctx context.Context, workspaceID, id, instanceID string) error
	FindByID(ctx context.Context, workspaceID, id string, scope Scope) (*Group, error)
	FindByJID(ctx context.Context, workspaceID, jid string) (*Group, error)
	FindByIDs(ctx context.Context, workspaceID string, ids []string, scope Scope) ([]*Group, error)
	Exists(ctx context.Context, workspaceID string) (bool, error)
	List(ctx context.Context, f GroupFilter) (*shared.PaginatedResult[*Group], error)
	Categories(ctx context.Context, workspaceID string, scope Scope) ([]string, error)
	NextSeriesSequence(ctx context.Context, workspaceID, seriesID string) (int, error)
}

type MemberFilter struct {
	GroupID     string
	Scope       Scope
	Status      MemberStatus
	AdminsOnly  bool
	Search      string
	LeadLinked  *bool
	InOtherOnly bool
	Pagination  shared.Pagination
}

type MemberRow struct {
	Member
	LeadName    string `json:"leadName,omitempty"`
	ContactName string `json:"contactName,omitempty"`
	PictureURL  string `json:"pictureUrl,omitempty"`
	OtherGroups int    `json:"otherGroups"`
}

type MemberCounts struct {
	Active      int64 `json:"active"`
	Left30d     int64 `json:"left30d"`
	Removed30d  int64 `json:"removed30d"`
	OursRemoved int64 `json:"oursRemoved"`
	WithLead    int64 `json:"withLead"`
	Hidden      int64 `json:"hidden"`
}

type MemberRepository interface {
	All(ctx context.Context, groupID string) ([]Member, error)
	Apply(ctx context.Context, g *Group, diff MemberDiff, at time.Time) error
	List(ctx context.Context, workspaceID string, f MemberFilter) (*shared.PaginatedResult[*MemberRow], error)
	Counts(ctx context.Context, groupID string, since time.Time) (MemberCounts, error)
	ByPersonKeys(ctx context.Context, groupID string, keys []string) ([]Member, error)
	ByPeople(ctx context.Context, workspaceID string, keys []string, scope Scope) ([]Member, error)
	SetLead(ctx context.Context, groupID, personKey, leadID string) error
	Names(ctx context.Context, groupID string) (map[string]string, error)
	GroupsOf(ctx context.Context, workspaceID string, personKeys []string, scope Scope) (map[string][]GroupRef, error)
}

type GroupRef struct {
	GroupID string `json:"groupId"`
	Subject string `json:"subject"`
}

type StatsScope struct {
	WorkspaceID string
	Scope       Scope
}

type GroupTotals struct {
	Groups     int64           `json:"groups"`
	Followed   int64           `json:"followed"`
	Open       int64           `json:"open"`
	Closed     int64           `json:"closed"`
	Categories []CategoryCount `json:"categories"`
}

type CategoryCount struct {
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

type PeopleTotals struct {
	Memberships int64 `json:"memberships"`
	Unique      int64 `json:"unique"`
}

type GroupCount struct {
	GroupID string `json:"groupId"`
	Subject string `json:"subject"`
	Count   int64  `json:"count"`
}

type GroupGrowth struct {
	GroupID string `json:"groupId"`
	Subject string `json:"subject"`
	Joins   int64  `json:"joins"`
	Leaves  int64  `json:"leaves"`
	Net     int64  `json:"net"`
}

type ResponseTimePoint struct {
	Day        time.Time `json:"day"`
	AvgMinutes float64   `json:"avgMinutes"`
	Samples    int64     `json:"samples"`
}

type GroupActivity struct {
	Messages     int64      `json:"messages"`
	LastActivity *time.Time `json:"lastActivity,omitempty"`
	Joins        int64      `json:"joins"`
	Leaves       int64      `json:"leaves"`
}

type PeopleFilter struct {
	StatsScope
	Search     string
	MinGroups  int
	Pagination shared.Pagination
}

type Person struct {
	PersonKey  string     `json:"personKey"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone,omitempty"`
	JID        string     `json:"jid"`
	LeadID     string     `json:"leadId,omitempty"`
	LeadName   string     `json:"leadName,omitempty"`
	GroupCount int        `json:"groupCount"`
	Groups     []GroupRef `json:"groups"`
	LastJoinAt *time.Time `json:"lastJoinAt,omitempty"`
}

type MembershipSort string

const (
	MembershipByName     MembershipSort = "name"
	MembershipByMembers  MembershipSort = "members"
	MembershipByJoins    MembershipSort = "joins"
	MembershipByLeaves   MembershipSort = "leaves"
	MembershipByChurn    MembershipSort = "churn"
	MembershipByNet      MembershipSort = "net"
	MembershipByMessages MembershipSort = "messages"
	MembershipByGrowth   MembershipSort = "growth"
)

type MembershipFilter struct {
	StatsScope
	Window     Window
	Origin     Origin
	Category   *string
	Search     string
	Sort       MembershipSort
	Desc       bool
	Pagination shared.Pagination
}

type MembershipStat struct {
	GroupID           string     `json:"groupId"`
	Subject           string     `json:"subject"`
	Origin            Origin     `json:"origin"`
	Category          string     `json:"category,omitempty"`
	Numbers           int64      `json:"numbers"`
	Members           int64      `json:"members"`
	Base              int64      `json:"startMembers"`
	Joins             int64      `json:"joins"`
	Leaves            int64      `json:"leaves"`
	Net               int64      `json:"net"`
	ChurnRate         *float64   `json:"churnRate"`
	GrowthRate        *float64   `json:"growthRate"`
	Messages          int64      `json:"messages"`
	LastActivity      *time.Time `json:"lastActivity,omitempty"`
	LastMemberEventAt *time.Time `json:"lastMemberEventAt,omitempty"`
}

type MembershipSummary struct {
	Groups       int      `json:"groups"`
	StartMembers int64    `json:"startMembers"`
	ChurnRate    *float64 `json:"churnRate"`
	GrowthRate   *float64 `json:"growthRate"`
	Members      int64    `json:"members"`
	Joins        int64    `json:"joins"`
	Leaves       int64    `json:"leaves"`
	Net          int64    `json:"net"`
	Messages     int64    `json:"messages"`
}

type MembershipPage struct {
	Page     *shared.PaginatedResult[*MembershipStat] `json:"page"`
	Summary  MembershipSummary                        `json:"summary"`
	From     time.Time                                `json:"from"`
	To       time.Time                                `json:"to"`
	Timezone string                                   `json:"timezone"`
}

type StatsReader interface {
	GroupTotals(ctx context.Context, s StatsScope) (GroupTotals, error)
	PeopleTotals(ctx context.Context, s StatsScope) (PeopleTotals, error)
	DailyMessages(ctx context.Context, s StatsScope, w Window, loc *time.Location) (map[string]DailyCount, error)
	TopGroupsByMessages(ctx context.Context, s StatsScope, w Window, limit int) ([]GroupCount, error)
	ResponseTimes(ctx context.Context, s StatsScope, w Window, loc *time.Location) (map[string]ResponseTimePoint, error)
	DailyGrowth(ctx context.Context, s StatsScope, groupID string, w Window, loc *time.Location) (map[string]DailyCount, error)
	TopGrowing(ctx context.Context, s StatsScope, w Window, limit int) ([]GroupGrowth, error)
	GroupActivity(ctx context.Context, workspaceID string, groupIDs []string, since time.Time) (map[string]GroupActivity, error)
	CrossGroupPeople(ctx context.Context, f PeopleFilter) (*shared.PaginatedResult[*Person], error)
	Membership(ctx context.Context, f MembershipFilter) ([]MembershipStat, error)
}

type AnalysisSort string

const (
	AnalysisByRecent AnalysisSort = "recent"
	AnalysisByOldest AnalysisSort = "oldest"
	AnalysisByName   AnalysisSort = "name"
	AnalysisByNameZA AnalysisSort = "name_desc"
)

type AnalysisFilter struct {
	WorkspaceID string
	Scope       Scope
	Status      AnalysisStatus
	Trigger     Trigger
	GroupID     string
	Search      string
	Sort        AnalysisSort
	Pagination  shared.Pagination
}

type AnalysisRow struct {
	Analysis
	GroupSubject string `json:"groupSubject"`
	Runs         int64  `json:"runs"`
}

type AnalysisRepository interface {
	Create(ctx context.Context, a *Analysis) error
	Save(ctx context.Context, a *Analysis) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*Analysis, error)
	List(ctx context.Context, f AnalysisFilter) (*shared.PaginatedResult[*AnalysisRow], error)
	AddPending(ctx context.Context, groupID string, n int) error
	ClaimNext(ctx context.Context, now time.Time, owner string, leaseUntil time.Time) (*Analysis, error)
	Extend(ctx context.Context, id, owner string, leaseUntil time.Time) error
	Finish(ctx context.Context, a *Analysis, owner string, consumed int) error
	Counts(ctx context.Context, s StatsScope) (total, active int64, err error)
	GroupsWithActive(ctx context.Context, workspaceID string, groupIDs []string) (map[string]bool, error)
}

type ThemeFilter struct {
	StatsScope
	Since      time.Time
	Search     string
	Pagination shared.Pagination
}

type ThemeAggregate struct {
	Theme       string     `json:"theme"`
	Occurrences int64      `json:"occurrences"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	Groups      []GroupRef `json:"groups"`
}

type RunTotals struct {
	Runs       int64 `json:"runs"`
	Messages   int64 `json:"messages"`
	HighAlerts int64 `json:"highAlerts"`
}

type GroupSentiment struct {
	GroupID  string `json:"groupId"`
	Subject  string `json:"subject"`
	Positive int64  `json:"positive"`
	Neutral  int64  `json:"neutral"`
	Negative int64  `json:"negative"`
}

type Insight struct {
	AnalysisID   string     `json:"analysisId"`
	AnalysisName string     `json:"analysisName"`
	GroupID      string     `json:"groupId"`
	Message      string     `json:"message"`
	AlertLevel   AlertLevel `json:"alertLevel"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type RunRepository interface {
	Create(ctx context.Context, r *Run) error
	ListByAnalysis(ctx context.Context, workspaceID, analysisID string, p shared.Pagination) (*shared.PaginatedResult[*Run], error)
	FindByID(ctx context.Context, workspaceID, analysisID, runID string) (*Run, error)
	Since(ctx context.Context, workspaceID, analysisID string, since time.Time) ([]Run, error)
	RecentForGroups(ctx context.Context, workspaceID string, groupIDs []string, since time.Time, perGroup int) (map[string][]Run, error)
	NextCycle(ctx context.Context, analysisID string) (int, error)
	Totals(ctx context.Context, s StatsScope) (RunTotals, error)
	SentimentTally(ctx context.Context, s StatsScope, since time.Time) (SentimentTally, error)
	SentimentByGroup(ctx context.Context, s StatsScope, since time.Time, limit int) ([]GroupSentiment, error)
	Themes(ctx context.Context, f ThemeFilter) (*shared.PaginatedResult[*ThemeAggregate], error)
	RecentInsights(ctx context.Context, s StatsScope, limit int) ([]Insight, error)
}

type JoinRepository interface {
	CreateMany(ctx context.Context, tasks []JoinTask) error
	ClaimNext(ctx context.Context, now time.Time, owner string, leaseUntil time.Time) (*JoinTask, error)
	Save(ctx context.Context, t *JoinTask) error
	FailRemaining(ctx context.Context, planID, reason string) error
	CancelPlan(ctx context.Context, workspaceID, planID string, scope Scope) (int64, error)
	Recent(ctx context.Context, workspaceID string, scope Scope, since time.Time) ([]JoinTask, error)
	LastLink(ctx context.Context, workspaceID, groupJID string) (string, error)
}

type TranscriptLine struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Author     string    `json:"author"`
	AuthorName string    `json:"authorName,omitempty"`
	Text       string    `json:"text"`
	Outbound   bool      `json:"outbound"`
}

type MessageReader interface {
	GroupConversation(ctx context.Context, instanceID, groupJID string) (string, error)
	TextAfter(ctx context.Context, conversationID string, after Cursor, limit int) ([]TranscriptLine, error)
	Between(ctx context.Context, conversationID string, from, to time.Time, offset, limit int) ([]TranscriptLine, error)
}

type GroupCampaign struct {
	CampaignID     string     `json:"campaignId"`
	Name           string     `json:"name"`
	CampaignStatus string     `json:"campaignStatus"`
	InstanceID     string     `json:"instanceId"`
	EntryStatus    string     `json:"entryStatus"`
	ErrorMessage   string     `json:"errorMessage,omitempty"`
	SentAt         *time.Time `json:"sentAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type CampaignReader interface {
	ForGroup(ctx context.Context, workspaceID, groupJID string, scope Scope, limit int) ([]GroupCampaign, error)
	RunningForGroups(ctx context.Context, workspaceID string, groupJIDs []string, scope Scope) (map[string]int, error)
}
