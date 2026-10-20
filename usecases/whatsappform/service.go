package whatsappform_usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/notification"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

var errIncomplete = errors.New("whatsapp forms: a required dependency is missing")

type Actor = conversation.Viewer

type Phones interface {
	FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type OwnedPhones interface {
	OwnedPhones(ctx context.Context, workspaceID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type Definitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type Assets interface {
	Load(ctx context.Context, workspaceID string, mediaIDs []string) (wf.Assets, error)
}

type FlowSender interface {
	SendFlowMessage(ctx context.Context, businessPhoneID string, in conversation.SendFlowMessageInput) (string, error)
}

type ConversationTarget struct {
	EntryID         string
	EntryType       string
	ConversationID  string
	LeadID          string
	BusinessPhoneID string
	ContactNumber   string
	WindowOpen      bool
}

type Conversations interface {
	Target(ctx context.Context, workspaceID, entryID string) (ConversationTarget, error)
}

type Scopes interface {
	ReadScope(userID, workspaceID string, isAdmin bool) (wf.ReadScope, bool)
}

type OutboundForm struct {
	WorkspaceID     string
	EntryID         string
	EntryType       string
	ConversationID  string
	BusinessPhoneID string
	From            string
	To              string
	MessageID       string
	Body            string
	Metadata        map[string]any
	SentBy          string
	Automated       bool
	At              time.Time
}

type History interface {
	RecordFormMessage(ctx context.Context, out OutboundForm) error
}

type ProfileWriter interface {
	Update(ctx context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error)
}

type ConsentWriter interface {
	Record(ctx context.Context, workspaceID, leadID string, change lead.ConsentChange) (lead_usecase.ConsentOutcome, error)
}

type StoredFile struct {
	WorkspaceID     string
	BusinessPhoneID string
	EntryID         string
	EntryType       string
	ResponseID      string
	FieldKey        string
	File            wf.File
}

type MediaStore interface {
	StoreFormFile(ctx context.Context, f StoredFile) (string, error)
}

type RunResumer interface {
	ResumeForm(ctx context.Context, runID string, data map[string]any) error
}

type FormTrigger struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	FormID      string
	ResponseID  string
	Data        map[string]any
}

type TriggerFirer interface {
	FormSubmitted(ctx context.Context, in FormTrigger) error
}

type LeadNames interface {
	Names(ctx context.Context, workspaceID string, ids []string) (map[string]string, error)
}

type Deps struct {
	Forms         wf.FormRepository
	Versions      wf.VersionRepository
	Publications  wf.PublicationRepository
	Sends         wf.SendRepository
	Responses     wf.ResponseRepository
	Gateway       wf.FlowsGateway
	Phones        Phones
	OwnedPhones   OwnedPhones
	Permissions   workspace.PermissionChecker
	Definitions   Definitions
	Assets        Assets
	Sender        FlowSender
	Conversations Conversations
	Access        shared.EntryAccessChecker
	Scopes        Scopes
	History       History
	Profiles      ProfileWriter
	Consents      ConsentWriter
	Media         MediaStore
	Runs          RunResumer
	Triggers      TriggerFirer
	LeadNames     LeadNames
	Automations   AutomationCatalog
	Notices       notification.EpisodePublisher
	Now           func() time.Time
	NewID         func() string
	Random        io.Reader
}

type Service struct {
	deps Deps
}

func New(deps Deps) (*Service, error) {
	required := []struct {
		name   string
		absent bool
	}{
		{"forms", deps.Forms == nil}, {"versions", deps.Versions == nil}, {"publications", deps.Publications == nil},
		{"sends", deps.Sends == nil}, {"responses", deps.Responses == nil}, {"gateway", deps.Gateway == nil},
		{"phones", deps.Phones == nil}, {"owned phones", deps.OwnedPhones == nil}, {"permissions", deps.Permissions == nil},
		{"definitions", deps.Definitions == nil}, {"assets", deps.Assets == nil}, {"sender", deps.Sender == nil},
		{"conversations", deps.Conversations == nil}, {"access", deps.Access == nil}, {"scopes", deps.Scopes == nil},
		{"history", deps.History == nil}, {"profiles", deps.Profiles == nil}, {"consents", deps.Consents == nil},
		{"media", deps.Media == nil}, {"runs", deps.Runs == nil}, {"triggers", deps.Triggers == nil},
		{"lead names", deps.LeadNames == nil}, {"automations", deps.Automations == nil}, {"notices", deps.Notices == nil},
	}
	for _, r := range required {
		if r.absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, r.name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewID == nil {
		deps.NewID = uuid.NewString
	}
	if deps.Random == nil {
		deps.Random = rand.Reader
	}
	return &Service{deps: deps}, nil
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) holds(a Actor, key workspace.CapabilityKey) bool {
	return workspace.HoldsCapability(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, key)
}

func (s *Service) require(a Actor, keys ...workspace.CapabilityKey) error {
	if strings.TrimSpace(a.WorkspaceID) == "" || strings.TrimSpace(a.UserID) == "" {
		return wf.ErrWorkspaceRequired
	}
	for _, key := range keys {
		if !s.holds(a, key) {
			return wf.ErrForbidden
		}
	}
	return nil
}

func (s *Service) readScope(a Actor) (wf.ReadScope, error) {
	scope, allowed := s.deps.Scopes.ReadScope(a.UserID, a.WorkspaceID, a.IsAdmin)
	if !allowed {
		return wf.ReadScope{}, wf.ErrForbidden
	}
	scope.Resolved = true
	return scope, nil
}

func (s *Service) leadDefinitions(workspaceID string) ([]*customfield.Definition, error) {
	defs, err := s.deps.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return defs, nil
}
