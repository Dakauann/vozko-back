package analysis

import (
	"time"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
)

// TRANSITIONAL SHIM.
//
// The conversation taxonomy moved to domain/comment_analysis, which is becoming
// the single analysis engine for every channel (see
// AUDIENCE_ANALYSIS_UNIFICATION_PLAN.md). The definitions are NOT duplicated
// here: these are aliases, so this package's remaining callers keep compiling
// while they are migrated one at a time. This whole package is deleted once the
// last one moves.

type (
	Interest      = ca.Interest
	Disposition   = ca.Disposition
	Qualification = ca.Qualification
	NextAction    = ca.NextAction
	// Sentiment is shared across channels (domain/shared/sentiment.go); the
	// alias keeps this package's persisted values unchanged.
	Sentiment = shared.Sentiment
)

const (
	InterestInterested    = ca.InterestInterested
	InterestNotInterested = ca.InterestNotInterested
	InterestUndecided     = ca.InterestUndecided

	DispositionSale        = ca.DispositionSale
	DispositionFillingInfo = ca.DispositionFillingInfo
	DispositionCallback    = ca.DispositionCallback
	DispositionDeclined    = ca.DispositionDeclined
	DispositionNoAnswer    = ca.DispositionNoAnswer
	DispositionVoicemail   = ca.DispositionVoicemail
	DispositionPending     = ca.DispositionPending

	SentimentPositive = shared.SentimentPositive
	SentimentNeutral  = shared.SentimentNeutral
	SentimentNegative = shared.SentimentNegative

	QualificationHotLead  = ca.QualificationHotLead
	QualificationWarmLead = ca.QualificationWarmLead
	QualificationColdLead = ca.QualificationColdLead

	NextActionScheduleCallback = ca.NextActionScheduleCallback
	NextActionSendWhatsApp     = ca.NextActionSendWhatsApp
	NextActionClose            = ca.NextActionClose
	NextActionEscalate         = ca.NextActionEscalate
	NextActionContinue         = ca.NextActionContinue
)

type Analysis struct {
	ID                string           `json:"id"`
	EntryID           string           `json:"entryId"`
	EntryType         shared.EntryType `json:"entryType"`
	Interest          Interest         `json:"interest"`
	ProductInterest   *string          `json:"productInterest,omitempty"`
	Disposition       Disposition      `json:"disposition"`
	Sentiment         Sentiment        `json:"sentiment"`
	Qualification     Qualification    `json:"qualification"`
	NextAction        NextAction       `json:"nextAction"`
	Summary           string           `json:"summary"`
	AttendanceQuality int              `json:"attendanceQuality"`
	MessageCount      int              `json:"messageCount,omitempty"`
	CreatedAt         time.Time        `json:"createdAt"`
}

type AnalysisInput struct {
	EntryID      string
	EntryType    shared.EntryType
	Transcript   string
	MessageCount int
}

type AIAnalysisResponse struct {
	Interest          string  `json:"interest"`
	ProductInterest   *string `json:"product_interest"`
	Disposition       string  `json:"disposition"`
	Sentiment         string  `json:"sentiment"`
	Qualification     string  `json:"qualification"`
	NextAction        string  `json:"next_action"`
	Summary           string  `json:"summary"`
	AttendanceQuality int     `json:"attendance_quality"`
}

type Repository interface {
	Create(analysis *Analysis) error
	FindByID(id string) (*Analysis, error)

	FindLatestByEntry(entryID string, entryType shared.EntryType) (*Analysis, error)

	FindLatestByEntries(entryIDs []string, entryType shared.EntryType) (map[string]*Analysis, error)

	ListByEntry(entryID string, entryType shared.EntryType) ([]Analysis, error)

	ListByLeadID(leadID string) ([]Analysis, error)

	ListByWhatsAppCampaign(whatsappCampaignID string) ([]Analysis, error)

	List(input ListAnalysisInput) (*shared.PaginatedResult[*Analysis], error)

	GetStats(input ListAnalysisInput) (*AnalysisStats, error)

	DeleteByEntry(entryID string, entryType shared.EntryType) error
}

type ListAnalysisInput struct {
	CampaignID           string
	WhatsAppCampaignID   string
	LeadID               string
	WorkspaceID          string
	EntryType            shared.EntryType
	Interest             Interest
	Disposition          Disposition
	Sentiment            Sentiment
	Qualification        Qualification
	NextAction           NextAction
	AttendanceQualityMin *int
	AttendanceQualityMax *int
	MessageCountMin      *int
	MessageCountMax      *int
	Options              shared.QueryOptions
}

type AnalysisStats struct {
	TotalAnalyses          int     `json:"totalAnalyses"`
	AvgAttendanceQuality   float64 `json:"avgAttendanceQuality"`
	MinAttendanceQuality   int     `json:"minAttendanceQuality"`
	MaxAttendanceQuality   int     `json:"maxAttendanceQuality"`
	TotalMessages          int     `json:"totalMessages"`
	AvgMessagesPerAnalysis float64 `json:"avgMessagesPerAnalysis"`

	InterestInterested    int `json:"interestInterested"`
	InterestNotInterested int `json:"interestNotInterested"`
	InterestUndecided     int `json:"interestUndecided"`

	DispositionSale        int `json:"dispositionSale"`
	DispositionFillingInfo int `json:"dispositionFillingInfo"`
	DispositionCallback    int `json:"dispositionCallback"`
	DispositionDeclined    int `json:"dispositionDeclined"`
	DispositionNoAnswer    int `json:"dispositionNoAnswer"`
	DispositionVoicemail   int `json:"dispositionVoicemail"`
	DispositionPending     int `json:"dispositionPending"`

	SentimentPositive int `json:"sentimentPositive"`
	SentimentNeutral  int `json:"sentimentNeutral"`
	SentimentNegative int `json:"sentimentNegative"`

	QualificationHotLead  int `json:"qualificationHotLead"`
	QualificationWarmLead int `json:"qualificationWarmLead"`
	QualificationColdLead int `json:"qualificationColdLead"`
}

type ListAnalysisUseCase interface {
	Execute(input ListAnalysisInput) (*shared.PaginatedResult[*Analysis], error)
}

type GetAnalysisStatsUseCase interface {
	Execute(input ListAnalysisInput) (*AnalysisStats, error)
}

type GetEntryAnalysisUseCase interface {
	Execute(entryID string, entryType shared.EntryType) (*Analysis, error)
}
