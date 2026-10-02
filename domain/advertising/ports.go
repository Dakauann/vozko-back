package advertising

import (
	"context"
	"time"
)

type RemoteAdAccount struct {
	MetaAccountID string
	Name          string
	BusinessID    string
	BusinessName  string
	Currency      string
	Timezone      string
	Status        MetaAccountStatus
	DisableReason int
	HasFunding    bool
	AmountSpent   int64
	SpendCap      int64
}

type RemotePage struct {
	PageID            string
	Name              string
	PictureURL        string
	WhatsAppNumber    string
	InstagramUserID   string
	InstagramUsername string
	CanAdvertise      bool
}

type RemoteLocation struct {
	Kind    LocationKind
	Key     string
	Name    string
	Region  string
	Country string
}

type StructureGateway interface {
	ListAdAccounts(ctx context.Context, token string) ([]RemoteAdAccount, error)
	GetAdAccount(ctx context.Context, token, metaAccountID string) (*RemoteAdAccount, error)
	ListPages(ctx context.Context, token string) ([]RemotePage, error)
	ListObjects(ctx context.Context, token, metaAccountID string, level Level) ([]*Object, error)
	GetObject(ctx context.Context, token, metaID string, level Level) (*Object, error)
	DailyInsights(ctx context.Context, token, metaAccountID string, r DateRange) ([]DailyInsight, error)
}

type PublishGateway interface {
	UploadImage(ctx context.Context, token, metaAccountID string, image []byte, fileName string) (string, error)
	UploadVideo(ctx context.Context, token, metaAccountID string, video []byte, fileName string) (string, error)
	VideoStatus(ctx context.Context, token, videoID string) (*RemoteVideo, error)
	CreateCampaign(ctx context.Context, token, metaAccountID string, spec CampaignSpec) (string, error)
	CreateAdSet(ctx context.Context, token, metaAccountID string, spec AdSetSpec) (string, error)
	CreateCreative(ctx context.Context, token, metaAccountID string, spec CreativeSpec) (string, error)
	CreateAd(ctx context.Context, token, metaAccountID string, spec AdSpec) (string, error)
	SetStatus(ctx context.Context, token, metaID string, status ConfiguredStatus) error
	DeleteObject(ctx context.Context, token, metaID string) error
}

type EditGateway interface {
	GetObjectDetail(ctx context.Context, token, metaID string, level Level) (*ObjectDetail, error)
	UpdateObject(ctx context.Context, token, metaID string, level Level, spec EditSpec) error
	CopyObject(ctx context.Context, token, metaID string, level Level, req CopyRequest) (string, error)
	SetSpendCap(ctx context.Context, token, metaAccountID string, cap int64) error
	RemoveSpendCap(ctx context.Context, token, metaAccountID string) error
}

type AssetGateway interface {
	SearchLocations(ctx context.Context, token, query string) ([]RemoteLocation, error)
	SearchTargeting(ctx context.Context, token, metaAccountID string, kind TargetingSearchKind, query string) ([]TargetingOption, error)
	EstimateReach(ctx context.Context, token, metaAccountID string, t Targeting, p Placements, goal OptimizationGoal) (*ReachEstimate, error)
	ListCatalogs(ctx context.Context, token, businessID string) ([]RemoteCatalog, error)
	ListApps(ctx context.Context, token, metaAccountID string) ([]RemoteApp, error)
	ListPagePosts(ctx context.Context, token, pageID string) ([]RemotePost, error)
	ListInstagramMedia(ctx context.Context, token, instagramUserID string) ([]RemotePost, error)
	ListInstantExperiences(ctx context.Context, token, pageID string) ([]RemoteInstantExperience, error)
}

type AudienceGateway interface {
	CustomAudienceTermsAccepted(ctx context.Context, token, metaAccountID string) (bool, error)
	ListAudiences(ctx context.Context, token, metaAccountID string) ([]Audience, error)
	CreateCustomerList(ctx context.Context, token, metaAccountID, name, description string) (string, error)
	AddCustomers(ctx context.Context, token, audienceID string, batch HashedCustomers, session CustomerSession) error
	CreateLookalike(ctx context.Context, token, metaAccountID string, draft LookalikeDraft) (string, error)
	DeleteAudience(ctx context.Context, token, audienceID string) error
}

type CustomerSession struct {
	ID        int64
	BatchSeq  int
	LastBatch bool
	TotalRows int
}

type LeadGateway interface {
	ListForms(ctx context.Context, token, pageID string) ([]LeadForm, error)
	CreateForm(ctx context.Context, token string, draft LeadFormDraft) (string, error)
	ArchiveForm(ctx context.Context, token, pageID, formID string) error
	ListLeads(ctx context.Context, token, pageID, formID string, since time.Time) ([]FormLead, error)
	GetLead(ctx context.Context, token, pageID, leadgenID string) (*FormLead, error)
	SubscribeLeadgen(ctx context.Context, token, pageID string) error
}

type RuleGateway interface {
	ListRules(ctx context.Context, token, metaAccountID string) ([]AutomatedRule, error)
	CreateRule(ctx context.Context, token, metaAccountID, currency string, rule AutomatedRule) (string, error)
	SetRuleStatus(ctx context.Context, token, ruleID string, status RuleStatus) error
	DeleteRule(ctx context.Context, token, ruleID string) error
	RuleHistory(ctx context.Context, token, ruleID string) ([]RuleRun, error)
}

type SignalGateway interface {
	ListPixels(ctx context.Context, token, metaAccountID string) ([]Pixel, error)
	CreatePixel(ctx context.Context, token, metaAccountID, name string) (string, error)
	DatasetForWABA(ctx context.Context, token, wabaID string) (string, error)
	SendEvents(ctx context.Context, token string, events []ConversionEvent) error
}

type InsightsGateway interface {
	LiveInsights(ctx context.Context, token, metaAccountID string, q LiveQuery) ([]LiveRow, error)
}

type TestGateway interface {
	CreateSplitTest(ctx context.Context, token, metaAccountID string, test SplitTest) (string, error)
	ListSplitTests(ctx context.Context, token, metaAccountID string) ([]SplitTest, error)
}

type WebhookGateway interface {
	SubscribeAccount(ctx context.Context, token, metaAccountID string) error
}

type MarketingGateway interface {
	StructureGateway
	WebhookGateway
	PublishGateway
	EditGateway
	AssetGateway
	AudienceGateway
	LeadGateway
	RuleGateway
	SignalGateway
	InsightsGateway
	TestGateway
}

type ImageGenerator interface {
	Generate(ctx context.Context, req ImageRequest) (*GeneratedImage, error)
}

type GrantRepository interface {
	Upsert(ctx context.Context, g *Grant) error
	FindByID(ctx context.Context, id string) (*Grant, error)
	MarkChecked(ctx context.Context, id string, scopes []string, granular map[string][]string, at time.Time) error
	Revoke(ctx context.Context, id string, at time.Time) error
}

type AccountRepository interface {
	Upsert(ctx context.Context, a *AdAccount) error
	FindByID(ctx context.Context, workspaceID, id string) (*AdAccount, error)
	FindByMetaAccountID(ctx context.Context, metaAccountID string) (*AdAccount, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*AdAccount, error)
	ListConnected(ctx context.Context, limit, offset int) ([]*AdAccount, error)
	SetConnection(ctx context.Context, id string, c Connection) error
	MarkSynced(ctx context.Context, id string, at time.Time) error
}

type ObjectQuery struct {
	WorkspaceID    string
	AdAccountID    string
	Level          Level
	CampaignIDs    []string
	AdSetIDs       []string
	MetaIDs        []string
	Search         string
	IncludeRemoved bool
}

type ObjectRepository interface {
	ReplaceLevel(ctx context.Context, accountID string, level Level, objects []*Object, at time.Time) error
	Upsert(ctx context.Context, object *Object) error
	Find(ctx context.Context, workspaceID, metaID string) (*Object, error)
	List(ctx context.Context, q ObjectQuery) ([]*Object, error)
}

type InsightRepository interface {
	ReplaceDays(ctx context.Context, accountID string, r DateRange, rows []DailyInsight) error
	Rows(ctx context.Context, accountID string, r DateRange) ([]DailyInsight, error)
	AdDay(ctx context.Context, adMetaID string, day time.Time) (*DailyInsight, error)
}

type AttributionRepository interface {
	ByAd(ctx context.Context, workspaceID string, adMetaIDs []string, from, to time.Time) ([]Attribution, error)
	Conversations(ctx context.Context, workspaceID, adMetaID string, from, to time.Time) (int64, error)
}

type PublishJobRepository interface {
	Create(ctx context.Context, job *PublishJob) error
	Find(ctx context.Context, workspaceID, id string) (*PublishJob, error)
	Save(ctx context.Context, job *PublishJob) error
	Claim(ctx context.Context, id string, from, to JobStatus) (bool, error)
	ListByWorkspace(ctx context.Context, workspaceID string, limit int) ([]*PublishJob, error)
	ListStale(ctx context.Context, before time.Time, limit int) ([]*PublishJob, error)
}

type NumberKind string

const (
	NumberOfficial   NumberKind = "official"
	NumberUnofficial NumberKind = "unofficial"
)

type WorkspaceNumber struct {
	Kind   NumberKind
	Label  string
	Number string
}

type NumberDirectory interface {
	List(ctx context.Context, workspaceID string) ([]WorkspaceNumber, error)
}

func NumbersLinkedTo(page RemotePage, numbers []WorkspaceNumber) []WorkspaceNumber {
	var linked []WorkspaceNumber
	for _, n := range numbers {
		if SameWhatsAppNumber(n.Number, page.WhatsAppNumber) {
			linked = append(linked, n)
		}
	}
	return linked
}

type SavedAudienceRepository interface {
	Create(ctx context.Context, s *SavedAudience) error
	Update(ctx context.Context, s *SavedAudience) error
	Delete(ctx context.Context, workspaceID, id string) error
	Find(ctx context.Context, workspaceID, id string) (*SavedAudience, error)
	List(ctx context.Context, workspaceID string) ([]*SavedAudience, error)
}

type TrackedForm struct {
	MetaID       string
	WorkspaceID  string
	AdAccountID  string
	PageID       string
	Name         string
	LastPolledAt *time.Time
}

type LeadFormRepository interface {
	Track(ctx context.Context, f *TrackedForm) error
	FindByMetaID(ctx context.Context, metaID string) (*TrackedForm, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*TrackedForm, error)
	ListAll(ctx context.Context, limit, offset int) ([]*TrackedForm, error)
	MarkPolled(ctx context.Context, metaID string, at time.Time) error
}

type FormLeadQuery struct {
	WorkspaceID string
	FormMetaID  string
	Limit       int
	Offset      int
}

type FormLeadRepository interface {
	Save(ctx context.Context, lead *FormLead) (bool, error)
	LinkLead(ctx context.Context, metaID, leadID string) error
	List(ctx context.Context, q FormLeadQuery) ([]*FormLead, int64, error)
}

type ConversionSettingsRepository interface {
	Get(ctx context.Context, workspaceID string) (*ConversionSettings, error)
	Save(ctx context.Context, s *ConversionSettings) error
	ListEnabled(ctx context.Context) ([]*ConversionSettings, error)
}

type PendingSignal struct {
	WorkspaceID string
	Signal      DealSignal
}

type ConversionOutbox interface {
	Pending(ctx context.Context, workspaceID string, since time.Time, limit int) ([]PendingSignal, error)
	Record(ctx context.Context, r ConversionRecord) error
	Recent(ctx context.Context, workspaceID string, limit int) ([]ConversionRecord, error)
}
