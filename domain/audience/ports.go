package audience

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type IngestInput struct {
	WorkspaceID      string
	Container        ContainerRef
	SubjectID        string
	ParentSubjectID  string
	AuthorExternalID string
	AuthorHandle     string
	Text             string
	Revision         string
	MessageCount     int
	OccurredAt       time.Time
	IsOurs           bool
}

type BacklogReader interface {
	CountWaiting(ctx context.Context, workspaceID string) (int, error)
}

type Ingestor interface {
	Enqueue(ctx context.Context, in IngestInput) error
}

type ConversationAnalysisObserver interface {
	AnalysisCreated(workspaceID, entryID, entryType, analysisID, disposition string, quality int)
}

type ConversationAnalysisState struct {
	EntryID   string    `json:"entryId"`
	EntryType string    `json:"entryType"`
	Pending   bool      `json:"pending"`
	Analysis  *Analysis `json:"analysis,omitempty"`
}

type ConversationAnalysisLive interface {
	AnalysisStateChanged(state ConversationAnalysisState)
}

type ContainerContext struct {
	Caption     string
	Permalink   string
	AccountName string
	PublishedAt *time.Time
}

type SourceAdapter interface {
	ReadTexts(ctx context.Context, ref ContainerRef, sourceCommentIDs []string) (map[string]string, error)
	ReadContainerContext(ctx context.Context, ref ContainerRef) (ContainerContext, error)
	ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ContainerSummary, error)
	FetchCommentsPage(ctx context.Context, ref ContainerRef, cursor string) (items []IngestInput, nextCursor string, err error)
}

type ContainerSummary struct {
	Ref           ContainerRef
	WorkspaceID   string
	CommentsCount int
}

type ClassifyRequest struct {
	WorkspaceID  string
	SubjectKind  SubjectKind
	Model        string
	Topics       TopicSet
	Context      ContainerContext
	Instructions string
	Batch        BatchPlan
}

type ClassifyResult struct {
	Results          []BatchResult
	FinishReason     string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

type Classifier interface {
	Classify(ctx context.Context, req ClassifyRequest) (*ClassifyResult, error)
}

type Scheduler interface {
	Stamp(ctx context.Context, ref ContainerRef, workspaceID string, now time.Time) error
	Hints(ctx context.Context) ([]Hint, error)
	Clear(ctx context.Context, ref ContainerRef) error
}

type Clock = shared.Clock

type SettingsResolver interface {
	Resolve(ctx context.Context, ref ContainerRef) (*Settings, error)
}

type EscalationDelivery struct {
	WorkspaceID   string
	RecipientID   string
	RecipientKind string
	ActorUserID   string
	Text          string
}

type EscalationSender interface {
	Send(ctx context.Context, in EscalationDelivery) error
}

type ReplyDraftRequest struct {
	WorkspaceID  string
	Model        string
	Instructions string
	Caption      string

	Comment      string
	AuthorHandle string
	Intent       Intent
	Stance       Stance
	Sentiment    string
	Language     string
	MaxLength    int
}

type ReplyDraftResult struct {
	Text             string
	Model            string
	RequestID        string
	PromptTokens     int
	CompletionTokens int
}

type ReplyDrafter interface {
	Draft(ctx context.Context, req ReplyDraftRequest) (*ReplyDraftResult, error)
}

type CommentReplier interface {
	ReplyToComment(ctx context.Context, workspaceID, accountID, sourceCommentID, text string) (replyID string, err error)
}

type AnalysisBroadcaster interface {
	BroadcastCommentsAnalyzed(event AnalysisBatchAnalyzed)
}

type RoleInferRequest struct {
	WorkspaceID  string
	Model        string
	Instructions string
	Comments     []string
}

type RoleInferResult struct {
	Role       AuthorRole
	Confidence string
	Rationale  string

	Model            string
	PromptTokens     int
	CompletionTokens int
}

type RoleInferrer interface {
	InferRole(ctx context.Context, req RoleInferRequest) (*RoleInferResult, error)
}

type AlertDelivery struct {
	WorkspaceID string
	Channel     AlertChannel
	Recipient   string

	BusinessPhoneID string
	TemplateID      string
	TemplateParams  []string
	Facts           []AlertFact

	InstanceID string
	Text       string

	ActorUserID string

	IdempotencyKey string
}

type AlertDispatcher interface {
	Dispatch(ctx context.Context, in AlertDelivery) error
}

type AlertEvaluator interface {
	EvaluateBatch(ctx context.Context, ref ContainerRef, workspaceID string, rows []*Analysis)
}

type AlertBriefRequest struct {
	WorkspaceID  string
	Model        string
	Instructions string
	Caption      string

	RuleName    string
	Measurement string
	Comment     string
	Stance      Stance
	Severity    int
}

type AlertBriefer interface {
	Brief(ctx context.Context, req AlertBriefRequest) (*AlertBriefing, error)
}
