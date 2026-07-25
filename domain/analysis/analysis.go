package analysis

import (
	"time"

	"vozko/domain/shared"
)

type Interest string

const (
	InterestInterested    Interest = "interested"
	InterestNotInterested Interest = "not_interested"
	InterestUndecided     Interest = "undecided"
)

func (i Interest) Valid() bool {
	switch i {
	case InterestInterested, InterestNotInterested, InterestUndecided:
		return true
	}
	return false
}

type Disposition string

const (
	DispositionSale        Disposition = "sale"
	DispositionFillingInfo Disposition = "filling_info"
	DispositionCallback    Disposition = "callback"
	DispositionDeclined    Disposition = "declined"
	DispositionNoAnswer    Disposition = "no_answer"
	DispositionVoicemail   Disposition = "voicemail"
	DispositionPending     Disposition = "pending"
)

func (d Disposition) Valid() bool {
	switch d {
	case DispositionSale, DispositionFillingInfo, DispositionCallback, DispositionDeclined,
		DispositionNoAnswer, DispositionVoicemail, DispositionPending:
		return true
	}
	return false
}

type Sentiment string

const (
	SentimentPositive Sentiment = "positive"
	SentimentNeutral  Sentiment = "neutral"
	SentimentNegative Sentiment = "negative"
)

func (s Sentiment) Valid() bool {
	switch s {
	case SentimentPositive, SentimentNeutral, SentimentNegative:
		return true
	}
	return false
}

type Qualification string

const (
	QualificationHotLead  Qualification = "hot_lead"
	QualificationWarmLead Qualification = "warm_lead"
	QualificationColdLead Qualification = "cold_lead"
)

func (q Qualification) Valid() bool {
	switch q {
	case QualificationHotLead, QualificationWarmLead, QualificationColdLead:
		return true
	}
	return false
}

type NextAction string

const (
	NextActionScheduleCallback NextAction = "schedule_callback"
	NextActionSendWhatsApp     NextAction = "send_whatsapp"
	NextActionClose            NextAction = "close"
	NextActionEscalate         NextAction = "escalate"
	NextActionContinue         NextAction = "continue"
)

func (n NextAction) Valid() bool {
	switch n {
	case NextActionScheduleCallback, NextActionSendWhatsApp,
		NextActionClose, NextActionEscalate, NextActionContinue:
		return true
	}
	return false
}

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
