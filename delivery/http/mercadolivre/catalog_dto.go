package mercadolivre

import "time"

type ListingResponse struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"accountId"`
	ItemID       string     `json:"itemId"`
	Title        string     `json:"title"`
	Permalink    string     `json:"permalink,omitempty"`
	Status       string     `json:"status"`
	ThumbnailURL string     `json:"thumbnailUrl,omitempty"`
	SyncedAt     *time.Time `json:"syncedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type ListingListResponse struct {
	Data       []ListingResponse `json:"data"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type QuestionAskerResponse struct {
	ContactID string `json:"contactId"`
	MLUserID  string `json:"mlUserId"`
	Nickname  string `json:"nickname,omitempty"`
	Blocked   bool   `json:"blocked"`
}

type QuestionResponse struct {
	ID               string                 `json:"id"`
	AccountID        string                 `json:"accountId"`
	ItemID           string                 `json:"itemId"`
	QuestionID       string                 `json:"questionId"`
	Text             string                 `json:"text"`
	Status           string                 `json:"status"`
	AnswerText       string                 `json:"answerText,omitempty"`
	AnswerStatus     string                 `json:"answerStatus,omitempty"`
	AnsweredBy       string                 `json:"answeredBy,omitempty"`
	AnsweredByUserID *string                `json:"answeredByUserId,omitempty"`
	RuleID           *string                `json:"ruleId,omitempty"`
	AskedAt          time.Time              `json:"askedAt"`
	AnsweredAt       *time.Time             `json:"answeredAt,omitempty"`
	SuspectedSpam    bool                   `json:"suspectedSpam"`
	Asker            *QuestionAskerResponse `json:"asker,omitempty"`
	Listing          *ListingResponse       `json:"listing,omitempty"`
}

type QuestionListResponse struct {
	Data       []QuestionResponse `json:"data"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type AnswerQuestionRequest struct {
	Text string `json:"text"`
}

type QuestionActionResponse struct {
	Status string `json:"status"`
}

type ResponseTimeSlotResponse struct {
	Minutes              int      `json:"minutes"`
	SalesPercentIncrease *float64 `json:"salesPercentIncrease,omitempty"`
}

type ResponseTimeResponse struct {
	Available            bool                      `json:"available"`
	Total                *ResponseTimeSlotResponse `json:"total,omitempty"`
	WeekdaysWorkingHours *ResponseTimeSlotResponse `json:"weekdaysWorkingHours,omitempty"`
	WeekdaysExtraHours   *ResponseTimeSlotResponse `json:"weekdaysExtraHours,omitempty"`
	Weekend              *ResponseTimeSlotResponse `json:"weekend,omitempty"`
}

type ReasonVarResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type ReasonTemplateResponse struct {
	ID   string              `json:"id"`
	Text string              `json:"text,omitempty"`
	Vars []ReasonVarResponse `json:"vars"`
}

type ReasonOptionResponse struct {
	ID           string                   `json:"id"`
	Description  string                   `json:"description,omitempty"`
	Type         string                   `json:"type,omitempty"`
	Enabled      bool                     `json:"enabled"`
	Actionable   bool                     `json:"actionable"`
	CharLimit    int                      `json:"charLimit,omitempty"`
	CapAvailable int                      `json:"capAvailable"`
	Templates    []ReasonTemplateResponse `json:"templates"`
}

type ReasonListResponse struct {
	Data []ReasonOptionResponse `json:"data"`
}

type ReasonVarRequest struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

type SendReasonRequest struct {
	OptionID   string             `json:"optionId"`
	TemplateID string             `json:"templateId"`
	Text       string             `json:"text"`
	Vars       []ReasonVarRequest `json:"vars"`
}

type SendReasonResponse struct {
	ProviderMessageID string `json:"providerMessageId"`
}

type PackItemResponse struct {
	ItemID       string  `json:"itemId"`
	Title        string  `json:"title"`
	Quantity     int     `json:"quantity"`
	UnitPrice    float64 `json:"unitPrice"`
	Currency     string  `json:"currency,omitempty"`
	ThumbnailURL string  `json:"thumbnailUrl,omitempty"`
	Permalink    string  `json:"permalink,omitempty"`
}

type PackSummaryResponse struct {
	Items             []PackItemResponse `json:"items"`
	OrderStatus       string             `json:"orderStatus"`
	ShippingStatus    string             `json:"shippingStatus"`
	ShippingSubstatus string             `json:"shippingSubstatus,omitempty"`
	Total             float64            `json:"total"`
	Currency          string             `json:"currency"`
	RefreshedAt       time.Time          `json:"refreshedAt"`
}
