package mercadolivre

import (
	"context"
	"strings"
	"time"
)

const (
	MaxItemsPerBatch       = 20
	MaxItemSearchLimit     = 100
	MaxQuestionSearchLimit = 50
	MaxThreadPageLimit     = 50
	MaxMissedFeedLimit     = 100
)

type TokenGrant struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	UserID       string
	Scopes       []string
}

func (g *TokenGrant) Validate() error {
	if g == nil || strings.TrimSpace(g.AccessToken) == "" || strings.TrimSpace(g.RefreshToken) == "" {
		return ErrCredentialsInvalid
	}
	if !ValidNumericID(g.UserID) {
		return ErrUserIDInvalid
	}
	if g.ExpiresIn <= 0 {
		return ErrCredentialsInvalid
	}
	if !HasRequiredScopes(g.Scopes) {
		return ErrScopeMissing
	}
	return nil
}

type RefreshCredentials struct {
	UserID       string
	RefreshToken string
}

type OAuthService interface {
	AuthorizeURL(state, codeChallenge string) (string, error)
	ExchangeCode(ctx context.Context, code, codeVerifier string) (*TokenGrant, error)
	Refresh(ctx context.Context, creds RefreshCredentials) (*TokenGrant, error)
	Revoke(ctx context.Context, creds Credentials) error
}

type User struct {
	ID            string
	Nickname      string
	FirstName     string
	LastName      string
	BrandName     string
	CorporateName string
	SiteID        string
	CountryID     string
	UserType      string
	Permalink     string
	Tags          []string
}

type UserService interface {
	Me(ctx context.Context, creds Credentials) (*User, error)
	GetUser(ctx context.Context, creds Credentials, userID string) (*User, error)
}

type ItemSearch struct {
	Status   string
	Offset   int
	Limit    int
	Scan     bool
	ScrollID string
}

type ItemIDPage struct {
	IDs      []string
	Total    int
	Offset   int
	Limit    int
	ScrollID string
}

type RemoteItem struct {
	ID        string
	SiteID    string
	SellerID  string
	Title     string
	Permalink string
	Thumbnail string
	Status    string
}

type ItemFailure struct {
	ID         string
	StatusCode int
}

type ItemService interface {
	SearchSellerItems(ctx context.Context, creds Credentials, search ItemSearch) (*ItemIDPage, error)
	GetItems(ctx context.Context, creds Credentials, ids []string) ([]*RemoteItem, []ItemFailure, error)
}

type RemoteAnswer struct {
	Text      string
	Status    AnswerStatus
	CreatedAt time.Time
}

type RemoteQuestion struct {
	ID                 string
	ItemID             string
	SellerID           string
	FromUserID         string
	Text               string
	Status             QuestionStatus
	Answer             *RemoteAnswer
	Hold               bool
	SuspectedSpam      bool
	DeletedFromListing bool
	CreatedAt          time.Time
}

type QuestionSearch struct {
	ItemID string
	Status QuestionStatus
	Offset int
	Limit  int
}

type QuestionPage struct {
	Questions []*RemoteQuestion
	Total     int
	Offset    int
	Limit     int
}

type ResponseTimeSlot struct {
	Minutes              int
	SalesPercentIncrease *float64
}

type ResponseTime struct {
	Available            bool
	Total                *ResponseTimeSlot
	WeekdaysWorkingHours *ResponseTimeSlot
	WeekdaysExtraHours   *ResponseTimeSlot
	Weekend              *ResponseTimeSlot
}

type QuestionService interface {
	GetQuestion(ctx context.Context, creds Credentials, questionID string) (*RemoteQuestion, error)
	SearchQuestions(ctx context.Context, creds Credentials, search QuestionSearch) (*QuestionPage, error)
	Answer(ctx context.Context, creds Credentials, questionID, text string) (*RemoteQuestion, error)
	DeleteQuestion(ctx context.Context, creds Credentials, questionID string) error
	BlockAsker(ctx context.Context, creds Credentials, askerID string) error
	UnblockAsker(ctx context.Context, creds Credentials, askerID string) error
	QuestionBlacklist(ctx context.Context, creds Credentials, offset, limit int) (*BlacklistPage, error)
	ResponseTime(ctx context.Context, creds Credentials) (*ResponseTime, error)
}

type MessageStatus string

const (
	MessageAvailable          MessageStatus = "available"
	MessagePendingReview      MessageStatus = "pending_review"
	MessageModerated          MessageStatus = "moderated"
	MessageRejected           MessageStatus = "rejected"
	MessagePendingTranslation MessageStatus = "pending_translation"
)

func (s MessageStatus) Known() bool {
	switch s {
	case MessageAvailable, MessagePendingReview, MessageModerated, MessageRejected, MessagePendingTranslation:
		return true
	}
	return false
}

func (s MessageStatus) Final() bool {
	return s == MessageAvailable || s == MessageModerated || s == MessageRejected
}

type RemoteAttachment struct {
	ID           string
	OriginalName string
	ContentType  string
	Size         int64
}

type RemoteMessage struct {
	ID               string
	FromUserID       string
	ToUserID         string
	Text             string
	Status           MessageStatus
	ModerationStatus string
	ModerationReason string
	CreatedAt        time.Time
	ReadAt           *time.Time
	Attachments      []RemoteAttachment
	ConversationPath string
	PackKey          string
}

type ThreadQuery struct {
	PackKey string
	Offset  int
	Limit   int
}

type Thread struct {
	Status                 string
	Substatus              string
	Path                   string
	ClaimIDs               []string
	ShippingID             string
	SellerMaxMessageLength int
	Messages               []*RemoteMessage
	Total                  int
	Offset                 int
	Limit                  int
}

func (t *Thread) State(agentMediated *bool, readAt time.Time) (ThreadState, error) {
	return NewThreadState(t.Status, t.Substatus, agentMediated, readAt)
}

type SendMessage struct {
	PackKey       string
	ToUserID      string
	Text          string
	AttachmentIDs []string
}

type SentMessage struct {
	ID               string
	Status           MessageStatus
	ModerationStatus string
	ModerationReason string
	PendingReview    bool
	CreatedAt        time.Time
}

type UnreadThread struct {
	PackKey  string
	SellerID string
	Count    int
}

type MessageService interface {
	Thread(ctx context.Context, creds Credentials, query ThreadQuery) (*Thread, error)
	ThreadForOperator(ctx context.Context, creds Credentials, query ThreadQuery) (*Thread, error)
	GetMessage(ctx context.Context, creds Credentials, messageID string) (*RemoteMessage, error)
	Send(ctx context.Context, creds Credentials, msg SendMessage) (*SentMessage, error)
	Unread(ctx context.Context, creds Credentials) ([]UnreadThread, error)
}

type AttachmentUpload struct {
	FileName    string
	ContentType string
	Data        []byte
}

type AttachmentDownload struct {
	Data        []byte
	ContentType string
}

type AttachmentService interface {
	Upload(ctx context.Context, creds Credentials, upload AttachmentUpload) (attachmentID string, err error)
	Download(ctx context.Context, creds Credentials, attachmentID string) (*AttachmentDownload, error)
}

type RemotePack struct {
	ID         string
	Status     string
	BuyerID    string
	ShipmentID string
	OrderIDs   []string
}

type RemoteOrderItem struct {
	ItemID     string
	Title      string
	Quantity   int
	UnitPrice  float64
	CurrencyID string
}

type RemoteOrder struct {
	ID          string
	PackID      string
	Status      string
	BuyerID     string
	SellerID    string
	ShipmentID  string
	CurrencyID  string
	TotalAmount float64
	Items       []RemoteOrderItem
	Tags        []string
}

type RemoteShipment struct {
	ID             string
	Status         string
	Substatus      string
	LogisticMode   string
	LogisticType   string
	TrackingNumber string
}

type PackService interface {
	GetPack(ctx context.Context, creds Credentials, packID string) (*RemotePack, error)
}

type OrderService interface {
	GetOrder(ctx context.Context, creds Credentials, orderID string) (*RemoteOrder, error)
}

type ShipmentService interface {
	GetShipment(ctx context.Context, creds Credentials, shipmentID string) (*RemoteShipment, error)
}

type MissedFeedQuery struct {
	AppID  string
	Topic  Topic
	SiteID string
	Offset int
	Limit  int
}

type MissedFeedService interface {
	MissedFeeds(ctx context.Context, creds Credentials, query MissedFeedQuery) ([]Notification, error)
}

const (
	ReasonOther           = "OTHER"
	ReasonRequestVariants = "REQUEST_VARIANTS"
	ReasonDeliveryPromise = "DELIVERY_PROMISE"
)

type ReasonVar struct {
	ID   string
	Type string
}

type ReasonTemplate struct {
	ID          string
	Text        string
	TextsBySite map[string]string
	Vars        []ReasonVar
}

type ReasonOption struct {
	ID           string
	Description  string
	Type         string
	Enabled      bool
	Actionable   bool
	CharLimit    int
	CapAvailable int
	Templates    []ReasonTemplate
}

type ReasonCap struct {
	OptionID     string
	CapAvailable int
}

type ReasonVarValue struct {
	ID    string
	Value int
}

type ReasonSend struct {
	OptionID   string
	TemplateID string
	Text       string
	Vars       []ReasonVarValue
}

type ActionGuideService interface {
	Options(ctx context.Context, creds Credentials, packKey string) ([]ReasonOption, error)
	Caps(ctx context.Context, creds Credentials, packKey string) ([]ReasonCap, error)
	SendOption(ctx context.Context, creds Credentials, packKey string, send ReasonSend) (*RemoteMessage, error)
}
